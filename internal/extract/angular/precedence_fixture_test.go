package angular_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

func TestPipePrecedenceFixture(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../../testdata/fixtures/angular-pipe-precedence")); err != nil {
		t.Fatal(err)
	}
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "precedence.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	opt := index.Options{Repository: "fixture", GeneratedAt: "2026-10-27T00:00:00Z", MaxFileSize: 2 << 20, Parser: parse.Options{Workers: 1, CacheDir: filepath.Join(t.TempDir(), "wasm")}}
	run := func() ([]byte, index.Summary) {
		t.Helper()
		var out bytes.Buffer
		summary, err := index.Emit(ctx, root, s, &out, opt)
		if err != nil {
			t.Fatal(err)
		}
		if err := ingest.Stream(ctx, bytes.NewReader(out.Bytes()), s); err != nil {
			t.Fatal(err)
		}
		return out.Bytes(), summary
	}
	first, _ := run()
	snapshot, err := s.Dump(ctx, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	matching := map[string]map[string]any{}
	for _, n := range snapshot.Nodes {
		names[n.Identity] = n.Name
		if a, ok := n.Attributes["angular"].(map[string]any); ok {
			if m, ok := a["template_matching"].(map[string]any); ok {
				matching[n.Name] = m
			}
		}
	}
	linked := map[string]int{"After": 2, "ClosedUnit": 1, "SpreadAfter": 1, "ForwardAfter": 1, "ObjectAfter": 1, "DeclaredHost": 1, "LegacyHost": 1}
	statuses := map[string]string{"Before": "shadowable", "Repeat": "shadowable", "OpenUnit": "shadowable", "OpenFirstUnit": "shadowable", "RepeatedExternalPackage": "shadowable", "FirstContribution": "shadowable", "DuplicateNames": "unknown", "OpenDuplicateNames": "shadowable", "UnknownName": "shadowable", "OpenUnknownName": "shadowable", "InvalidContexts": "invalid"}
	for host := range linked {
		statuses[host] = "linked"
	}
	for host, status := range statuses {
		m := matching[host]
		pipes, _ := m["pipes"].(map[string]any)
		want := 1
		if host == "After" {
			want = 2
		}
		if host == "InvalidContexts" {
			want = 3
		}
		for key, got := range pipes {
			expected := 0
			if key == "entries" || key == status {
				expected = want
			}
			if fmt.Sprint(got) != fmt.Sprint(expected) {
				t.Errorf("%s pipes.%s = %v, want %d", host, key, got, expected)
			}
		}
		if len(pipes) != 7 || fmt.Sprint(m["matches"]) != fmt.Sprint(linked[host]) {
			t.Errorf("%s matching: %v", host, m)
		}
	}
	edges := map[string]int{}
	for _, e := range snapshot.Edges {
		if e.Kind != "uses_pipe" {
			continue
		}
		host := names[e.Source]
		edges[host]++
		want := "LocalPipe"
		if host == "DeclaredHost" {
			want = "DeclaredPipe"
		}
		if host == "LegacyHost" {
			want = "LegacyPipe"
		}
		if names[e.Target] != want || e.Provenance != "fact" || e.Confidence != 1 || len(e.Evidence) != linked[host] {
			t.Errorf("unexpected pipe edge: %+v", e)
		}
		for _, ev := range e.Evidence {
			if ev.Span.EndColumn-ev.Span.StartColumn != len(map[string]string{"LocalPipe": "local", "DeclaredPipe": "declared", "LegacyPipe": "legacy"}[want]) {
				t.Errorf("evidence must cover only the pipe name: %+v", ev)
			}
		}
	}
	for host := range statuses {
		want := 0
		if linked[host] > 0 {
			want = 1
		}
		if edges[host] != want {
			t.Errorf("%s edges = %d, want %d", host, edges[host], want)
		}
	}
	golden.Check(t, "../../../testdata/fixtures/angular-pipe-precedence.golden.json", store.Snapshot{Nodes: snapshot.Nodes, Edges: snapshot.Edges, Diagnostics: snapshot.Diagnostics})
	again, replay := run()
	if replay.Parsed != 0 || !bytes.Equal(first, again) {
		t.Fatal("warm precedence differs", replay)
	}
	// Reverse an import without changing any pipe declaration or template.
	path := filepath.Join(root, "hosts.ts")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source = bytes.Replace(source, []byte("imports: [CommonModule, LocalPipe], template: '{{value | local}} {{other | local}}'"), []byte("imports: [LocalPipe, CommonModule], template: '{{value | local}} {{other | local}}'"), 1)
	if err := os.WriteFile(path, source, 0644); err != nil {
		t.Fatal(err)
	}
	_, edit := run()
	if edit.Parsed != 1 {
		t.Fatal("import edit reparsed other files", edit)
	}
	for _, n := range snapshot.Nodes {
		if n.Name == "After" {
			now, err := s.Edges(ctx, "fixture", []string{n.Identity}, []string{"uses_pipe"}, store.Outgoing)
			if err != nil || len(now) != 0 {
				t.Fatal("reordered import retained a pipe edge", now, err)
			}
		}
	}
}

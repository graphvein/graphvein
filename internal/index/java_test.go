package index_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

// TestJavaBestEffortGoldenAndReplay indexes the Java best-effort fixture
// (T021-B2; one case per contract row, see the fixture's README.md) and
// checks the golden, the file records' precision label (BE-1), the
// refused files (BE-2) and a warm replay that parses nothing.
func TestJavaBestEffortGoldenAndReplay(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/java-best-effort")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	data, first, _ := emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/java-best-effort.golden.json", store.Snapshot{Nodes: snapshot.Nodes, Edges: snapshot.Edges, Diagnostics: snapshot.Diagnostics})
	javaFiles := 0
	for _, f := range snapshot.Files {
		if f.Language == "java" {
			javaFiles++
			if f.Precision != "best_effort" {
				t.Errorf("%s: precision %q, want best_effort (BE-1)", f.Path, f.Precision)
			}
		}
	}
	if javaFiles != 12 || first.Parsed != 12 {
		t.Fatalf("java files %d, parsed %d, want 12", javaFiles, first.Parsed)
	}
	syntax := map[string]bool{}
	for _, d := range snapshot.Diagnostics {
		if d.Code == "syntax_error" {
			syntax[d.File] = true
		}
		if d.Code == "parse_failed" || d.Code == "extract_failed" {
			t.Fatal(d)
		}
	}
	for _, refused := range []string{"src/main/java/com/acme/app/Broken.java", "src/main/java/com/acme/app/Missing.java", "src/main/java/com/acme/secret/Vault.java", "tools/Scratch.java"} {
		if !syntax[refused] {
			t.Errorf("%s: no syntax_error warning", refused)
		}
		for _, n := range snapshot.Nodes {
			if n.File == refused && n.Kind != "file" {
				t.Errorf("BE-2 file claims %s", n.Identity)
			}
		}
		for _, e := range snapshot.Edges {
			if e.Source == "file:"+refused {
				t.Errorf("BE-2 file claims %s -> %s", e.Kind, e.Target)
			}
		}
	}
	// BE-8: the refused files declare packages com.acme.app (also declared
	// by readable files) and com.acme.secret, and Scratch.java none, so only
	// the import of com.acme.secret.Vault is hidden; the rest are JV-I8.
	externals, hidden := 0, 0
	for _, n := range snapshot.Nodes {
		if n.Kind == "external" {
			externals++
		}
	}
	for _, d := range snapshot.Diagnostics {
		if d.Code == "unresolved_import" && strings.Contains(d.Message, "may be declared in a file refused") {
			hidden++
			if !strings.Contains(d.Message, "com.acme.secret.Vault") || !strings.HasSuffix(d.Message, ": src/main/java/com/acme/secret/Vault.java") {
				t.Errorf("BE-8 warning %q", d.Message)
			}
		}
	}
	if externals != 4 || hidden != 1 {
		t.Errorf("externals %d, BE-8 warnings %d, want 4 and 1", externals, hidden)
	}
	warmData, warm, writes := emit(t, root, s, opt)
	if string(data) != string(warmData) || warm.Parsed != 0 || warm.Reused != first.Parsed || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0 {
		t.Fatal("java replay differs", warm, writes)
	}
}

// TestJavaBestEffortExternalGolden indexes the second Java fixture root,
// which has no refused or failed file, so its imports reach row JV-I8
// (external java: nodes) instead of row BE-8.
func TestJavaBestEffortExternalGolden(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/java-best-effort-external")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/java-best-effort-external.golden.json", store.Snapshot{Nodes: snapshot.Nodes, Edges: snapshot.Edges, Diagnostics: snapshot.Diagnostics})
	externals := 0
	for _, n := range snapshot.Nodes {
		if n.Kind == "external" {
			externals++
		}
	}
	if externals != 3 || len(snapshot.Diagnostics) != 0 {
		t.Fatalf("externals %d, diagnostics %v", externals, snapshot.Diagnostics)
	}
}

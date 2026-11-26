package index_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

// TestDartBestEffortGoldenAndReplay indexes the Dart best-effort fixture
// (T021-B4; one case per contract row, see the fixture's README.md) and
// checks the golden, the file records' precision label (BE-1), the refused
// file (BE-2), the directive edges and a warm replay that parses nothing.
func TestDartBestEffortGoldenAndReplay(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/dart-best-effort")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	data, first, _ := emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/dart-best-effort.golden.json", store.Snapshot{Nodes: snapshot.Nodes, Edges: snapshot.Edges, Diagnostics: snapshot.Diagnostics})
	dartFiles := 0
	for _, f := range snapshot.Files {
		if f.Language == "dart" {
			dartFiles++
			if f.Precision != "best_effort" {
				t.Errorf("%s: precision %q, want best_effort (BE-1)", f.Path, f.Precision)
			}
		}
	}
	if dartFiles != 11 || first.Parsed != 11 {
		t.Fatalf("dart files %d, parsed %d, want 11", dartFiles, first.Parsed)
	}
	const refused = "lib/src/broken.dart"
	codes := map[string]int{}
	for _, d := range snapshot.Diagnostics {
		codes[d.Code]++
		if d.Code == "syntax_error" && d.File != refused {
			t.Errorf("unexpected syntax_error in %s", d.File)
		}
		if d.Code == "parse_failed" || d.Code == "extract_failed" {
			t.Fatal(d)
		}
	}
	if codes["syntax_error"] != 1 || codes["unresolved_import"] != 3 {
		t.Errorf("diagnostics %v", codes)
	}
	externals := []string{}
	for _, n := range snapshot.Nodes {
		if n.File == refused && n.Kind != "file" {
			t.Errorf("BE-2 file claims %s", n.Identity)
		}
		if n.Kind == "external" {
			externals = append(externals, n.QualifiedName)
		}
	}
	if got := strings.Join(externals, " "); got != "dart:async dart:math pub:http pub:web" {
		t.Errorf("externals %s", got)
	}
	warmData, warm, writes := emit(t, root, s, opt)
	if string(data) != string(warmData) || warm.Parsed != 0 || warm.Reused != first.Parsed || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0 {
		t.Fatal("dart replay differs", warm, writes)
	}
}

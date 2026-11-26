package index_test

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

// TestCSharpBestEffortGoldenAndReplay indexes the C# best-effort fixture
// (T021-B3; one case per contract row, see the fixture's README.md) and
// checks the golden, the file records' precision label (BE-1), the refused
// file (BE-2) and a warm replay that parses nothing.
func TestCSharpBestEffortGoldenAndReplay(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/csharp-best-effort")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	data, first, _ := emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/csharp-best-effort.golden.json", store.Snapshot{Nodes: snapshot.Nodes, Edges: snapshot.Edges, Diagnostics: snapshot.Diagnostics})
	csFiles := 0
	for _, f := range snapshot.Files {
		if f.Language == "c_sharp" {
			csFiles++
			if f.Precision != "best_effort" {
				t.Errorf("%s: precision %q, want best_effort (BE-1)", f.Path, f.Precision)
			}
		}
	}
	if csFiles != 6 || first.Parsed != 6 {
		t.Fatalf("c# files %d, parsed %d, want 6", csFiles, first.Parsed)
	}
	const refused = "src/Broken.cs"
	syntax := false
	for _, d := range snapshot.Diagnostics {
		if d.Code == "syntax_error" && d.File == refused {
			syntax = true
		}
		if d.Code == "parse_failed" || d.Code == "extract_failed" {
			t.Fatal(d)
		}
	}
	if !syntax {
		t.Errorf("%s: no syntax_error warning", refused)
	}
	externals := 0
	for _, n := range snapshot.Nodes {
		if n.File == refused && n.Kind != "file" {
			t.Errorf("unexpected node %s (BE-2)", n.Identity)
		}
		if n.Kind == "external" {
			externals++
			if strings.Contains(n.Identity, "Hidden") {
				t.Errorf("external node %s names the refused file's namespace (BE-8)", n.Identity)
			}
		}
	}
	if externals != 6 {
		t.Errorf("externals %d, want 6 (CS-I2 outside the refused file's namespace)", externals)
	}
	for _, e := range snapshot.Edges {
		if e.Source == "file:"+refused {
			t.Errorf("BE-2 file claims %s -> %s", e.Kind, e.Target)
		}
	}
	warmData, warm, writes := emit(t, root, s, opt)
	if string(data) != string(warmData) || warm.Parsed != 0 || warm.Reused != first.Parsed || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0 {
		t.Fatal("c# replay differs", warm, writes)
	}
}

// TestCSharpBestEffortExternalGolden indexes the second C# fixture root,
// which has no refused or failed file, so its usings reach row CS-I2
// (external dotnet: nodes) instead of row BE-8. Its Web project's usings of
// a Razor folder namespace and of generated code under a repository
// namespace are CS-I4 warnings, not externals (T021-F1).
func TestCSharpBestEffortExternalGolden(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/csharp-best-effort-external")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/csharp-best-effort-external.golden.json", store.Snapshot{Nodes: snapshot.Nodes, Edges: snapshot.Edges, Diagnostics: snapshot.Diagnostics})
	var externals []string
	for _, n := range snapshot.Nodes {
		if n.Kind == "external" {
			externals = append(externals, n.Identity)
		}
	}
	var warned []string
	for _, d := range snapshot.Diagnostics {
		warned = append(warned, d.File+" "+d.Code+": "+d.Message)
	}
	wantExternals := []string{
		"external:.#dotnet%3AApp.Components",
		"external:.#dotnet%3ASystem",
		"external:.#dotnet%3ASystem.Linq",
		"external:.#dotnet%3ASystem.Math",
		"external:.#dotnet%3ASystem.Text.Json.JsonSerializer",
	}
	wantWarned := []string{
		"src/Web/Startup.cs unresolved_import: using Shop.Web.Components is not declared by a scanned C# file, but the repository declares namespace Shop.Web or one under it",
		"src/Web/Startup.cs unresolved_import: using Shop.Web.Services.Generated is not declared by a scanned C# file, but the repository declares namespace Shop.Web.Services or one under it",
	}
	if !reflect.DeepEqual(externals, wantExternals) || !reflect.DeepEqual(warned, wantWarned) {
		t.Fatalf("externals %q, diagnostics %q", externals, warned)
	}
}

// TestCSharpBestEffortHiddenGolden indexes the third C# fixture root, whose
// refused files use block namespaces (Messages.cs) or no namespace at all
// (Global.cs). Row BE-8 hides a plain using only when it is equal to, under
// or a dotted prefix of a namespace name in a refused file's text, and every
// static and alias using (a global-namespace type may be its target); the
// main root shows the file-scoped case.
func TestCSharpBestEffortHiddenGolden(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/csharp-best-effort-hidden")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/csharp-best-effort-hidden.golden.json", store.Snapshot{Nodes: snapshot.Nodes, Edges: snapshot.Edges, Diagnostics: snapshot.Diagnostics})
	codes := map[string]int{}
	hiddenBy := map[string]string{}
	for _, d := range snapshot.Diagnostics {
		codes[d.Code]++
		if d.Code == "unresolved_import" {
			using, where, _ := strings.Cut(d.Message, " is not declared by a readable scanned file, but may be declared in a file refused for invalid syntax or a failed parse: ")
			hiddenBy[using] = where
		}
	}
	var externals []string
	for _, n := range snapshot.Nodes {
		if n.Kind == "external" {
			externals = append(externals, n.Identity)
		}
	}
	if want := []string{"external:.#dotnet%3AGenX.Tools", "external:.#dotnet%3ASystem"}; !reflect.DeepEqual(externals, want) {
		t.Errorf("externals %v, want %v (CS-I2 outside the refused files' namespaces)", externals, want)
	}
	want := map[string]string{
		"using Gen.Messages":        "src/Gen/Messages.cs",
		"using Gen":                 "src/Gen/Messages.cs",
		"using Wire":                "src/Gen/Messages.cs",
		"using static Helpers.Text": "src/Gen/Global.cs and 1 more",
		"using Newtonsoft.Json":     "src/Gen/Global.cs and 1 more",
	}
	if codes["syntax_error"] != 2 || !reflect.DeepEqual(hiddenBy, want) || len(snapshot.Diagnostics) != 7 {
		t.Fatalf("diagnostics %v", snapshot.Diagnostics)
	}
}

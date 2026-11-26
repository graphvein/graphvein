package query

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
)

func TestColumnConversion(t *testing.T) {
	for _, tc := range []struct {
		source           string
		offset           int
		utf16, codepoint int
		exact            bool
	}{
		{"abc", 2, 2, 2, true}, {"éx", 2, 1, 1, true}, {"€x", 3, 1, 1, true},
		{"😀x", 4, 2, 1, true}, {"é€😀", 9, 4, 3, true}, {"😀", 1, 1, 1, false},
		{"x", 2, 2, 2, false}, {"x", -1, -1, -1, false},
	} {
		for _, unit := range []string{"utf16", "codepoint", "byte"} {
			want := tc.offset
			if unit == "utf16" {
				want = tc.utf16
			}
			if unit == "codepoint" {
				want = tc.codepoint
			}
			got, exact := convert([]string{tc.source}, 1, tc.offset, unit)
			if got != want || exact != tc.exact {
				t.Fatalf("%q %s %d: %d %v", tc.source, unit, tc.offset, got, exact)
			}
		}
	}
	// Line 1 drops a byte order mark in every unit; inside it fails.
	for _, unit := range []string{"utf16", "codepoint", "byte"} {
		if got, ok := convert([]string{"\uFEFF😀x"}, 1, 7, unit); !ok || got != map[string]int{"utf16": 2, "codepoint": 1, "byte": 4}[unit] {
			t.Fatalf("BOM %s: %d %v", unit, got, ok)
		}
		if _, ok := convert([]string{"\uFEFFx"}, 1, 2, unit); ok {
			t.Fatalf("inside BOM %s", unit)
		}
	}
	for _, line := range []int{0, 2} {
		if _, ok := convert([]string{"x"}, line, 0, "utf16"); ok {
			t.Fatal("invalid line")
		}
	}
}

func TestIDECurrentSourceAndReadCache(t *testing.T) {
	root := t.TempDir()
	source := []byte("é😀x\r\nlast\r\n")
	p := filepath.Join(root, "a #%.go")
	if err := os.WriteFile(p, source, 0644); err != nil {
		t.Fatal(err)
	}
	span := contract.Span{StartLine: 1, StartColumn: 6, EndLine: 2, EndColumn: 4}
	n := contract.Node{File: "a #%.go", Span: &span}
	l := links{root: root, config: config.DefaultIDE(), files: map[string]sourceFile{}}
	got := l.node(n, contract.ContentHash(source))
	if !got.IDE.Exact || got.IDE.Column != 4 || !strings.Contains(got.IDE.Link, "a%20%23%25.go:1:4") {
		t.Fatal(got.IDE)
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if !l.node(n, contract.ContentHash(source)).IDE.Exact {
		t.Fatal("file read more than once")
	}
	if got := l.node(n, contract.ContentHash([]byte("different"))); got.IDE.Exact || got.IDE.Column != 7 {
		t.Fatal(got.IDE)
	}
	span.EndColumn = 99
	if got := l.node(n, contract.ContentHash(source)); got.IDE.Exact || got.IDE.Column != 7 {
		t.Fatal(got.IDE)
	}
	if l.node(contract.Node{File: "."}, "").IDE != nil {
		t.Fatal("synthetic link")
	}
	cfg := config.IDE{LinkTemplate: "editor:{rel_path}:{column}:{end_line}:{end_column}", ColumnUnit: "codepoint", ColumnBase: 0}
	span.EndColumn = 4
	l.config = cfg
	if got := l.node(n, contract.ContentHash(source)); got.IDE.Link != "editor:a%20%23%25.go:2:2:4" {
		t.Fatal(got.IDE)
	}
}

func TestIDEPathEncoding(t *testing.T) {
	if got := encodePath("C:/src/a # %/é.go"); got != "C:/src/a%20%23%20%25/%C3%A9.go" {
		t.Fatal(got)
	}
}

func TestAbsURLPath(t *testing.T) {
	for _, tc := range []struct{ abs, want string }{
		{"/home/u/my project/a.ts", "/home/u/my%20project/a.ts"},
		{`C:\src\my project\a#b.ts`, "/C:/src/my%20project/a%23b.ts"},
		{"C:/src/a.ts", "/C:/src/a.ts"},
		// A UNC path keeps the two slashes that name its server.
		{`\\server\share\a.ts`, "//server/share/a.ts"},
	} {
		if got := absURLPath(tc.abs); got != tc.want {
			t.Errorf("absURLPath(%q) = %q, want %q", tc.abs, got, tc.want)
		}
	}
}

// TestPresetLinks fills every preset's template for a POSIX and a Windows
// path, independent of the OS running the test.
func TestPresetLinks(t *testing.T) {
	posix, windows := "/home/u/my project/a.ts", `C:\src\my project\a#b.ts`
	want := map[string][2]string{
		"vscode":          {"vscode://file/home/u/my%20project/a.ts:3:5", "vscode://file/C:/src/my%20project/a%23b.ts:3:5"},
		"vscode-insiders": {"vscode-insiders://file/home/u/my%20project/a.ts:3:5", "vscode-insiders://file/C:/src/my%20project/a%23b.ts:3:5"},
		"cursor":          {"cursor://file/home/u/my%20project/a.ts:3:5", "cursor://file/C:/src/my%20project/a%23b.ts:3:5"},
		"vscodium":        {"vscodium://file/home/u/my%20project/a.ts:3:5", "vscodium://file/C:/src/my%20project/a%23b.ts:3:5"},
		"jetbrains":       {"idea://open?file=/home/u/my%20project/a.ts&line=3&column=5", "idea://open?file=C:/src/my%20project/a%23b.ts&line=3&column=5"},
		"zed":             {"zed://file/home/u/my%20project/a.ts:3:5", "zed://fileC:/src/my%20project/a%23b.ts:3:5"},
	}
	presets := config.Presets()
	if len(presets) != len(want) {
		t.Fatalf("%d presets, want %d", len(presets), len(want))
	}
	for _, p := range presets {
		w, ok := want[p.Name]
		if !ok {
			t.Errorf("unexpected preset %s", p.Name)
			continue
		}
		for i, abs := range []string{posix, windows} {
			if got := fillTemplate(p.LinkTemplate, abs, "a.ts", 3, 5, 4, 1); got != w[i] {
				t.Errorf("%s %s: %s, want %s", p.Name, abs, got, w[i])
			}
		}
	}
	// A user template with {abs_path} keeps its exact earlier output: the
	// double slash on POSIX, the single one before a drive letter.
	legacy := "vscode://file/{abs_path}:{line}:{column}"
	if got := fillTemplate(legacy, posix, "a.ts", 3, 5, 4, 1); got != "vscode://file//home/u/my%20project/a.ts:3:5" {
		t.Error(got)
	}
	if got := fillTemplate(legacy, windows, "a.ts", 3, 5, 4, 1); got != "vscode://file/C:/src/my%20project/a%23b.ts:3:5" {
		t.Error(got)
	}
}

// TestPresenterWithReader presents through the supplied reader only.
func TestPresenterWithReader(t *testing.T) {
	source := []byte("ab\n")
	span := contract.Span{StartLine: 1, StartColumn: 1, EndLine: 1, EndColumn: 2}
	n := contract.Node{File: "x.go", Span: &span}
	reads := 0
	p := NewPresenterWithReader(t.TempDir(), config.DefaultIDE(), func(file string) ([]byte, error) {
		reads++
		if file != "x.go" {
			t.Fatalf("read %q", file)
		}
		return source, nil
	})
	if got := p.Node(n, contract.ContentHash(source)); !got.IDE.Exact || got.IDE.Column != 2 {
		t.Fatal(got.IDE)
	}
	if p.FileState("x.go", contract.ContentHash(source)) != FileUnchanged || reads != 1 {
		t.Fatalf("reads %d", reads)
	}
	failing := NewPresenterWithReader(t.TempDir(), config.DefaultIDE(), func(string) ([]byte, error) { return nil, os.ErrPermission })
	if failing.FileState("x.go", contract.ContentHash(source)) != FileMissing || failing.Node(n, contract.ContentHash(source)).IDE.Exact {
		t.Fatal("a failed read is not missing")
	}
}

// presetIDE returns the settings of every preset by name.
func presetIDE(t *testing.T) map[string]config.IDE {
	t.Helper()
	ide := map[string]config.IDE{}
	for _, name := range config.PresetNames() {
		cfg, ok := config.PresetIDE(name)
		if !ok {
			t.Fatalf("preset %s", name)
		}
		ide[name] = cfg
	}
	return ide
}

// TestJetBrainsColumnCountsUTF16 checks the jetbrains preset against
// IntelliJ's navigation: OpenFileDescriptor(line - 1, column) builds its
// offset with DocumentUtil.calculateOffset, which walks the line with
// charAt, one UTF-16 unit per column (a tab adds the tab size), and the
// caret moves to that offset. In "😀 = foo" foo starts at byte 7; the emoji
// is two UTF-16 units and " = " three, so foo is UTF-16 offset 5, and with
// the preset's 0-based column the link says column=5 (codepoints would say
// 4, one character early).
func TestJetBrainsColumnCountsUTF16(t *testing.T) {
	source := []byte("😀 = foo\n")
	span := contract.Span{StartLine: 1, StartColumn: 7, EndLine: 1, EndColumn: 10}
	n := contract.Node{File: "a.go", Span: &span}
	p := NewPresenterWithReader("/r", presetIDE(t)["jetbrains"], func(string) ([]byte, error) { return source, nil })
	want := IDELink{Link: "idea://open?file=/r/a.go&line=1&column=5", Line: 1, Column: 5, Exact: true}
	if got := *p.Node(n, contract.ContentHash(source)).IDE; got != want {
		t.Fatalf("%+v, want %+v", got, want)
	}
}

// TestBOMColumn checks every preset on line 1 of a file with a byte order
// mark: contract columns count its three bytes, the editors drop it from
// the document, so "\uFEFFfunc x" at byte 7 is column 4 of "func x" (the
// space), 5 in the 1-based presets and 4 in jetbrains. A column inside the
// mark cannot be converted; byte 0 is the start of the line.
func TestBOMColumn(t *testing.T) {
	source := []byte("\uFEFFfunc x\nfunc y\n")
	hash := contract.ContentHash(source)
	ide := presetIDE(t)
	present := func(cfg config.IDE, line, column int) IDELink {
		span := contract.Span{StartLine: line, StartColumn: column, EndLine: line, EndColumn: column}
		p := NewPresenterWithReader("/r", cfg, func(string) ([]byte, error) { return source, nil })
		return *p.Node(contract.Node{File: "a.go", Span: &span}, hash).IDE
	}
	for _, tc := range []struct {
		editor string
		want   IDELink
	}{
		{"vscode", IDELink{Link: "vscode://file/r/a.go:1:5", Line: 1, Column: 5, Exact: true}},
		{"vscode-insiders", IDELink{Link: "vscode-insiders://file/r/a.go:1:5", Line: 1, Column: 5, Exact: true}},
		{"cursor", IDELink{Link: "cursor://file/r/a.go:1:5", Line: 1, Column: 5, Caveat: config.CaveatColumnUnitAssumed}},
		{"vscodium", IDELink{Link: "vscodium://file/r/a.go:1:5", Line: 1, Column: 5, Exact: true}},
		{"jetbrains", IDELink{Link: "idea://open?file=/r/a.go&line=1&column=4", Line: 1, Column: 4, Exact: true}},
		{"zed", IDELink{Link: "zed://file/r/a.go:1:5", Line: 1, Column: 5, Exact: true}},
	} {
		if got := present(ide[tc.editor], 1, 7); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.editor, got, tc.want)
		}
	}
	if len(ide) != 6 {
		t.Fatalf("%d presets, the BOM table covers 6", len(ide))
	}
	if got := present(ide["vscode"], 1, 0); !got.Exact || got.Column != 1 {
		t.Errorf("BOM start: %+v", got)
	}
	if got := present(ide["vscode"], 1, 1); got.Exact {
		t.Errorf("inside the BOM: %+v", got)
	}
	// Only line 1 carries the mark: "func y" at byte 4 is column 5.
	if got := present(ide["vscode"], 2, 4); !got.Exact || got.Column != 5 {
		t.Errorf("line 2: %+v", got)
	}
}

// TestCursorColumnAssumed checks rule 14 for Cursor: its column unit is
// assumed from VS Code, so a link converted from unchanged source is not
// exact and says why; the same template set explicitly behaves the same.
func TestCursorColumnAssumed(t *testing.T) {
	source := []byte("é = x\n")
	span := contract.Span{StartLine: 1, StartColumn: 5, EndLine: 1, EndColumn: 6}
	cursor := presetIDE(t)["cursor"]
	explicit := config.IDE{LinkTemplate: cursor.LinkTemplate, ColumnUnit: "utf16", ColumnBase: 1}
	for _, cfg := range []config.IDE{cursor, explicit} {
		p := NewPresenterWithReader("/r", cfg, func(string) ([]byte, error) { return source, nil })
		want := IDELink{Link: "cursor://file/r/a.go:1:5", Line: 1, Column: 5, Caveat: config.CaveatColumnUnitAssumed}
		if got := *p.Node(contract.Node{File: "a.go", Span: &span}, contract.ContentHash(source)).IDE; got != want {
			t.Errorf("%+v, want %+v", got, want)
		}
		// A changed file: the byte column, inexact, no caveat.
		if got := *p.Node(contract.Node{File: "a.go", Span: &span}, "sha256:other").IDE; got.Exact || got.Caveat != "" || got.Column != 6 {
			t.Errorf("changed: %+v", got)
		}
	}
}

// TestJetBrainsTabLineOnly checks that a jetbrains link after a tab, or on
// a file that changed since indexing, names the line only (IntelliJ expands
// a tab to a tab size the server does not know; a changed file's byte
// column is not IntelliJ's), while the same template on an unchanged line
// without a tab before the node and every other preset keep their columns.
func TestJetBrainsTabLineOnly(t *testing.T) {
	ide := presetIDE(t)
	source := []byte("x := 1\n\tgo run()\nab\tcd\n")
	present := func(cfg config.IDE, line, column int, hash string) IDELink {
		span := contract.Span{StartLine: line, StartColumn: column, EndLine: line, EndColumn: column + 1}
		p := NewPresenterWithReader("/r", cfg, func(string) ([]byte, error) { return source, nil })
		return *p.Node(contract.Node{File: "a.go", Span: &span}, hash).IDE
	}
	hash := contract.ContentHash(source)
	for _, tc := range []struct {
		name         string
		cfg          config.IDE
		line, column int
		hash         string
		want         IDELink
	}{
		{"tab-indent", ide["jetbrains"], 2, 1, hash, IDELink{Link: "idea://open?file=/r/a.go&line=2", Line: 2, Column: 0, LineOnly: true, Caveat: config.CaveatTabSize}},
		{"tab-mid-line", ide["jetbrains"], 3, 3, hash, IDELink{Link: "idea://open?file=/r/a.go&line=3", Line: 3, Column: 0, LineOnly: true, Caveat: config.CaveatTabSize}},
		{"tab-after-node", ide["jetbrains"], 3, 0, hash, IDELink{Link: "idea://open?file=/r/a.go&line=3&column=0", Line: 3, Column: 0, Exact: true}},
		{"no-tab", ide["jetbrains"], 1, 5, hash, IDELink{Link: "idea://open?file=/r/a.go&line=1&column=5", Line: 1, Column: 5, Exact: true}},
		// Review fix R2 C1: a changed file no longer keeps the raw byte column.
		{"changed-file", ide["jetbrains"], 1, 5, "sha256:other", IDELink{Link: "idea://open?file=/r/a.go&line=1", Line: 1, Column: 0, LineOnly: true}},
		{"invalid-position", ide["jetbrains"], 1, 99, hash, IDELink{Link: "idea://open?file=/r/a.go&line=1", Line: 1, Column: 0, LineOnly: true}},
		{"zed", ide["zed"], 2, 1, hash, IDELink{Link: "zed://file/r/a.go:2:2", Line: 2, Column: 2, Exact: true}},
		{"vscode", ide["vscode"], 2, 1, hash, IDELink{Link: "vscode://file/r/a.go:2:2", Line: 2, Column: 2, Exact: true}},
		{"vscode-changed", ide["vscode"], 2, 1, "sha256:other", IDELink{Link: "vscode://file/r/a.go:2:2", Line: 2, Column: 2}},
	} {
		if got := present(tc.cfg, tc.line, tc.column, tc.hash); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStrictConfig(t *testing.T) {
	for _, tc := range []struct{ name, input string }{
		{"unknown", "version: 1\nunknown: {}\n"},
		{"ide-mapping", "version: 1\nide: []\n"},
		{"ide-key", "version: 1\nide: {unknown: x}\n"},
		{"ide-duplicate", "version: 1\nide: {column_base: 0, column_base: 1}\n"},
		{"ide-type", "version: 1\nide: {link_template: 123}\n"},
		{"ide-unit", "version: 1\nide: {column_unit: characters}\n"},
		{"ide-base", "version: 1\nide: {column_base: 2}\n"},
		{"ide-base-type", "version: 1\nide: {column_base: '1'}\n"},
		{"ide-placeholder", "version: 1\nide: {link_template: '{abs_path}/{oops}'}\n"},
		{"ide-path", "version: 1\nide: {link_template: 'editor:{line}'}\n"},
		{"ide-alias", "version: 1\nide: &i {column_unit: &u utf16, link_template: *u}\n"},
		{"ide-editor-unknown", "version: 1\nide: sublime\n"},
		{"ide-editor-key-unknown", "version: 1\nide: {editor: vscode-oss}\n"},
		{"ide-editor-empty", "version: 1\nide: ''\n"},
		{"ide-editor-key-empty", "version: 1\nide: {editor: ''}\n"},
		{"ide-editor-int", "version: 1\nide: 5\n"},
		{"ide-editor-null", "version: 1\nide:\n"},
		{"ide-editor-bool", "version: 1\nide: {editor: true}\n"},
		{"ide-editor-alias", "version: 1\nrepository: &e cursor\nide: *e\n"},
		{"ide-editor-key-alias", "version: 1\nrepository: &e cursor\nide: {editor: *e}\n"},
		{"ide-editor-duplicate", "version: 1\nide: {editor: zed, editor: cursor}\n"},
		{"ide-editor-sequence", "version: 1\nide: [cursor]\n"},
		{"ide-label-empty", "version: 1\nide: {label: ''}\n"},
		{"ide-label-int", "version: 1\nide: {label: 7}\n"},
		{"ide-label-long", "version: 1\nide: {label: '" + strings.Repeat("x", 61) + "'}\n"},
		{"ide-label-control", "version: 1\nide: {label: \"Open\\tit\"}\n"},
		{"ide-label-newline", "version: 1\nide: {label: \"Open\\nit\"}\n"},
		{"ide-template-empty", "version: 1\nide: {link_template: ''}\n"},
		{"ide-unit-empty", "version: 1\nide: {column_unit: ''}\n"},
		{"ide-url-placeholder-typo", "version: 1\nide: {link_template: 'x://{abs_url}'}\n"},
		{"duplicate", "version: 1\nrepository: a\nrepository: b\n"},
		{"duplicate-version", "version: 1\nversion: 1\n"},
		{"wrong-version", "version: 2\n"},
		{"string-version", "version: '1'\n"},
		{"missing-version", "repository: a\n"},
		{"numeric-repository", "version: 1\nrepository: 123\n"},
		{"bool-repository", "version: 1\nrepository: true\n"},
		{"empty-repository", "version: 1\nrepository: ''\n"},
		{"null-repository", "version: 1\nrepository:\n"},
		{"nul-repository", "version: 1\nrepository: \"a\\0b\"\n"},
		{"ref-repository", "version: 1\nrepository: app@ref:abc\n"},
		{"changes-sequence", "version: 1\nchanges: []\n"},
		{"changes-unknown", "version: 1\nchanges: {journal: 3}\n"},
		{"changes-duplicate", "version: 1\nchanges: {max_pins: 2, max_pins: 3}\n"},
		{"changes-runs-zero", "version: 1\nchanges: {journal_runs: 0}\n"},
		{"changes-runs-string", "version: 1\nchanges: {journal_runs: '3'}\n"},
		{"changes-runs-large", "version: 1\nchanges: {journal_runs: 1001}\n"},
		{"changes-budget-zero", "version: 1\nchanges: {journal_budget: 0}\n"},
		{"changes-budget-negative", "version: 1\nchanges: {journal_budget: -1.5}\n"},
		{"changes-budget-nan", "version: 1\nchanges: {journal_budget: .nan}\n"},
		{"changes-budget-string", "version: 1\nchanges: {journal_budget: half}\n"},
		{"changes-pins-float", "version: 1\nchanges: {max_pins: 2.5}\n"},
		{"changes-refs-zero", "version: 1\nchanges: {max_ref_snapshots: 0}\n"},
		{"sequence", "- version: 1\n"},
		{"alias", "version: 1\nrepository: &r a\nother: *r\n"},
		{"documents", "version: 1\n---\nversion: 1\n"},
		{"empty-second-document", "version: 1\n---\n"},
		{"empty", ""},
		{"syntax", "version: [\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "graphvein.yaml")
			if err := os.WriteFile(path, []byte(tc.input), 0644); err != nil {
				t.Fatal(err)
			}
			_, err := Load(root)
			if err == nil || !strings.Contains(err.Error(), path) {
				t.Fatalf("error = %v", err)
			}
			if tc.name != "empty" && !strings.Contains(err.Error(), "line") {
				t.Fatalf("missing line: %v", err)
			}
		})
	}
}

func TestResolution(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"checkout-one", "other-checkout"} {
		dir := filepath.Join(root, name)
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
		id, err := ResolveRepository(dir, nil)
		if err != nil || id != name {
			t.Fatal(id, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "graphvein.yaml"), []byte("version: 1\nrepository: shared\n"), 0644); err != nil {
			t.Fatal(err)
		}
		id, err = ResolveRepository(dir, nil)
		if err != nil || id != "shared" {
			t.Fatal(id, err)
		}
		flag := "override"
		id, err = ResolveRepository(dir, &flag)
		if err != nil || id != flag {
			t.Fatal(id, err)
		}
		for _, invalid := range []string{"", "a\x00b"} {
			if _, err := ResolveRepository(dir, &invalid); err == nil {
				t.Fatal("accepted invalid override")
			}
		}
	}
	if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte("version: 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if id, err := ResolveRepository(root, nil); err != nil || id != filepath.Base(root) {
		t.Fatal(id, err)
	}
	flag := "override"
	if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte("version: 2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveRepository(root, &flag); err == nil {
		t.Fatal("override bypassed validation")
	}
}

func TestIDEConfigDefaultsAndOverrides(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		input string
		want  IDE
	}{
		{"version: 1\n", DefaultIDE()},
		{"version: 1\nide: {}\n", DefaultIDE()},
		{"version: 1\nide: {link_template: 'editor:{rel_path}:{end_line}:{end_column}', column_unit: byte, column_base: 0}\n", IDE{Editor: "vscode", LinkTemplate: "editor:{rel_path}:{end_line}:{end_column}", ColumnUnit: "byte", ColumnBase: 0}},
	} {
		if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte(tc.input), 0644); err != nil {
			t.Fatal(err)
		}
		got, err := Load(root)
		if err != nil || got.IDE != tc.want {
			t.Fatal(got, err)
		}
	}
}

// TestIDEPresets checks every preset's template, column unit, base and
// label (ADR 0044), through the scalar shorthand and the editor key.
func TestIDEPresets(t *testing.T) {
	want := map[string]IDE{
		"vscode":          {"vscode", "Open in VS Code", "vscode://file{abs_url_path}:{line}:{column}", "utf16", 1},
		"vscode-insiders": {"vscode-insiders", "Open in VS Code Insiders", "vscode-insiders://file{abs_url_path}:{line}:{column}", "utf16", 1},
		"cursor":          {"cursor", "Open in Cursor", "cursor://file{abs_url_path}:{line}:{column}", "utf16", 1},
		"vscodium":        {"vscodium", "Open in VSCodium", "vscodium://file{abs_url_path}:{line}:{column}", "utf16", 1},
		"jetbrains":       {"jetbrains", "Open in IntelliJ IDEA", "idea://open?file={abs_path}&line={line}&column={column}", "utf16", 0},
		"zed":             {"zed", "Open in Zed", "zed://file{abs_path}:{line}:{column}", "codepoint", 1},
	}
	if len(Presets()) != len(want) {
		t.Fatalf("%d presets", len(Presets()))
	}
	if DefaultIDE() != want["vscode"] {
		t.Fatalf("default %+v", DefaultIDE())
	}
	root := t.TempDir()
	for _, p := range Presets() {
		for _, input := range []string{"ide: " + p.Name, "ide: {editor: " + p.Name + "}"} {
			got := loadIDE(t, root, "version: 1\n"+input+"\n")
			if got != want[p.Name] {
				t.Errorf("%s: %+v, want %+v", input, got, want[p.Name])
			}
		}
	}
}

// TestIDEOverrides checks the per-field precedence: explicit key, then the
// preset, then the vscode default.
func TestIDEOverrides(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		input string
		want  IDE
	}{
		{"ide: {editor: jetbrains, label: Open in IDEA}", IDE{"jetbrains", "Open in IDEA", "idea://open?file={abs_path}&line={line}&column={column}", "utf16", 0}},
		{"ide: {editor: jetbrains, column_base: 1}", IDE{"jetbrains", "Open in IntelliJ IDEA", "idea://open?file={abs_path}&line={line}&column={column}", "utf16", 1}},
		{"ide: {editor: zed, column_unit: byte}", IDE{"zed", "Open in Zed", "zed://file{abs_path}:{line}:{column}", "byte", 1}},
		// An explicit template drops the preset label, even when it equals
		// the preset's template; an explicit label is kept.
		{"ide: {editor: cursor, link_template: 'cursor://file{abs_url_path}:{line}'}", IDE{"cursor", "", "cursor://file{abs_url_path}:{line}", "utf16", 1}},
		{"ide: {editor: zed, link_template: 'zed://file{abs_path}:{line}:{column}'}", IDE{"zed", "", "zed://file{abs_path}:{line}:{column}", "codepoint", 1}},
		{"ide: {link_template: 'subl://open?url=file://{abs_path}&line={line}', label: Open in Sublime Text}", IDE{"vscode", "Open in Sublime Text", "subl://open?url=file://{abs_path}&line={line}", "utf16", 1}},
		{"ide: {label: '" + strings.Repeat("é", 60) + "'}", IDE{"vscode", strings.Repeat("é", 60), "vscode://file{abs_url_path}:{line}:{column}", "utf16", 1}},
	} {
		if got := loadIDE(t, root, "version: 1\n"+tc.input+"\n"); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.input, got, tc.want)
		}
	}
	path := filepath.Join(root, "graphvein.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nide: sublime\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(root)
	if want := path + `: line 2: unknown ide editor "sublime" (want vscode, vscode-insiders, cursor, vscodium, jetbrains or zed)`; err == nil || err.Error() != want {
		t.Fatalf("error %v, want %s", err, want)
	}
}

// TestIDEDocsExamples decodes every complete graphvein.yaml example of the
// IDE section of docs/config.md, so the documentation cannot drift.
func TestIDEDocsExamples(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "config.md"))
	if err != nil {
		t.Fatal(err)
	}
	blocks := strings.Split(string(data), "```yaml\n")
	root := t.TempDir()
	checked := 0
	for _, b := range blocks[1:] {
		example, _, _ := strings.Cut(b, "```")
		if !strings.HasPrefix(example, "version: 1\n") || !strings.Contains(example, "ide:") {
			continue
		}
		if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte(example), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(root); err != nil {
			t.Errorf("docs/config.md example %q: %v", example, err)
		}
		checked++
	}
	if checked < 3 {
		t.Fatalf("only %d IDE examples found in docs/config.md", checked)
	}
}

func loadIDE(t *testing.T, root, input string) IDE {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(root)
	if err != nil {
		t.Fatalf("%s: %v", input, err)
	}
	return c.IDE
}

// TestPresetStatus checks that every preset states how far it was verified,
// with a status from the enum and one nonempty sentence (ADR 0050).
func TestPresetStatus(t *testing.T) {
	for _, p := range Presets() {
		switch p.Status {
		case StatusVerified, StatusPartlyVerified, StatusLimited:
		default:
			t.Errorf("%s: status %q", p.Name, p.Status)
		}
		if strings.TrimSpace(p.Note) == "" || !strings.HasSuffix(p.Note, ".") || strings.Contains(p.Note, "\n") {
			t.Errorf("%s: note %q", p.Name, p.Note)
		}
		got, ok := PresetIDE(p.Name)
		if !ok || got.Editor != p.Name || got.LinkTemplate != p.LinkTemplate || got.Label != p.Label {
			t.Errorf("%s: PresetIDE %+v", p.Name, got)
		}
	}
	if _, ok := PresetIDE(""); ok {
		t.Error("PresetIDE accepts an empty name")
	}
	if _, ok := PresetIDE("sublime"); ok {
		t.Error("PresetIDE accepts an unknown name")
	}
	if got := strings.Join(PresetNames(), ","); got != "vscode,vscode-insiders,cursor,vscodium,jetbrains,zed" {
		t.Errorf("names %s", got)
	}
}

// TestLineTemplate checks the line-only templates: only jetbrains has one
// (IntelliJ expands tabs in its column), it is its template without the
// column, and it is found by the template, not by the preset name.
func TestLineTemplate(t *testing.T) {
	for _, p := range Presets() {
		want := ""
		if p.Name == "jetbrains" {
			want = "idea://open?file={abs_path}&line={line}"
			if !strings.Contains(p.Note, "tab") {
				t.Errorf("jetbrains note lacks the tab caveat: %q", p.Note)
			}
		}
		if p.LineTemplate != want || LineTemplate(p.LinkTemplate) != want {
			t.Errorf("%s: line template %q / %q, want %q", p.Name, p.LineTemplate, LineTemplate(p.LinkTemplate), want)
		}
		if p.LineTemplate != "" && strings.Contains(p.LineTemplate, "{column}") {
			t.Errorf("%s: line template has a column", p.Name)
		}
	}
	if got := LineTemplate("x://{rel_path}:{line}:{column}"); got != "" {
		t.Errorf("custom template: %q", got)
	}
}

// TestColumnCaveat checks the presets whose column unit is assumed: only
// cursor (assumed from VS Code), found by its template, and its note says
// the column is not exact.
func TestColumnCaveat(t *testing.T) {
	for _, p := range Presets() {
		want := ""
		if p.Name == "cursor" {
			want = CaveatColumnUnitAssumed
			if !strings.Contains(p.Note, "not exact") {
				t.Errorf("cursor note lacks the column caveat: %q", p.Note)
			}
		}
		if p.ColumnCaveat != want || ColumnCaveat(p.LinkTemplate) != want {
			t.Errorf("%s: column caveat %q / %q, want %q", p.Name, p.ColumnCaveat, ColumnCaveat(p.LinkTemplate), want)
		}
	}
	if got := ColumnCaveat("x://{rel_path}:{line}:{column}"); got != "" {
		t.Errorf("custom template: %q", got)
	}
}

// TestIDEGiven tells Graphvein's built-in default (no file, no ide key, an
// empty mapping) from a repository setting, and a custom template from a
// preset.
func TestIDEGiven(t *testing.T) {
	root := t.TempDir()
	if c, err := Load(root); err != nil || c.IDEGiven || c.IDECustom {
		t.Fatalf("no file: %+v %v", c, err)
	}
	for _, tc := range []struct {
		input         string
		given, custom bool
	}{
		{"version: 1\n", false, false},
		{"version: 1\nide: {}\n", false, false},
		{"version: 1\nide: vscode\n", true, false},
		{"version: 1\nide: zed\n", true, false},
		{"version: 1\nide: {editor: cursor}\n", true, false},
		{"version: 1\nide: {column_unit: byte}\n", true, false},
		{"version: 1\nide: {link_template: 'x://{rel_path}'}\n", true, true},
		{"version: 1\nide: {editor: zed, link_template: 'zed://file{abs_path}:{line}:{column}'}\n", true, true},
	} {
		if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte(tc.input), 0o644); err != nil {
			t.Fatal(err)
		}
		c, err := Load(root)
		if err != nil || c.IDEGiven != tc.given || c.IDECustom != tc.custom {
			t.Errorf("%q: given %v custom %v, err %v", tc.input, c.IDEGiven, c.IDECustom, err)
		}
	}
}

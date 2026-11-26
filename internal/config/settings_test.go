package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// settingsGolden compares got with testdata/settings/name. A missing
// golden is written only when GRAPHVEIN_CREATE_GOLDENS=1 (never an
// existing one: a changed golden is edited by hand and explained).
func settingsGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", "settings", name)
	want, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) && os.Getenv("GRAPHVEIN_CREATE_GOLDENS") == "1" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(got) {
		t.Errorf("%s differs:\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}

func readSettingsInput(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "settings", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestSettingsEditGoldens checks rows ST-27 and ST-29: typed edits change
// only the lines of the edited entry (line comments, the file's four-space
// indentation, blank lines, keys outside the form and
// their comments all kept), a remove takes an emptied section with it, and
// an edit the splice cannot prove falls back to the re-encoded node tree
// with every comment kept. Every result loads.
func TestSettingsEditGoldens(t *testing.T) {
	cases := []struct {
		input, name string
		ops         []SettingOp
		reformatted bool
	}{
		{"commented.yaml", "labels", []SettingOp{{Key: "ui.labels", Value: "plain"}}, false},
		{"commented.yaml", "depth", []SettingOp{{Key: "index.shape_depth", Value: 8}}, false},
		{"commented.yaml", "editor-shorthand", []SettingOp{{Key: "ide.editor", Value: "zed"}}, false},
		{"commented.yaml", "flow-list", []SettingOp{{Key: "tests.include", Value: []string{"checks/**", "**/*.scenario.ts"}}}, false},
		{"commented.yaml", "block-list", []SettingOp{{Key: "tests.exclude", Value: []string{"a.ts", "b.ts"}}}, false},
		{"commented.yaml", "new-sections", []SettingOp{
			{Key: "openapi.base_paths", Value: []MapEntry{{"contracts", "/api"}, {"contracts/admin.yaml", "/admin/api"}}},
			{Key: "clients.link_generated_output", Value: true},
			{Key: "architecture.hide", Value: []string{"examples/"}},
		}, false},
		{"commented.yaml", "new-key-in-section", []SettingOp{{Key: "tests.exclude", Remove: true}, {Key: "openapi.include", Value: []string{"contracts/**"}}}, false},
		{"commented.yaml", "remove-section", []SettingOp{{Key: "ui.labels", Remove: true}, {Key: "index.shape_depth", Remove: true}, {Key: "ide.editor", Remove: true}}, false},
		{"listcomments.yaml", "replace", []SettingOp{{Key: "tests.include", Value: []string{"checks/**"}}}, false},
		{"flow.yaml", "labels", []SettingOp{{Key: "ui.labels", Value: "technical"}}, true},
	}
	for _, c := range cases {
		t.Run(c.input+"/"+c.name, func(t *testing.T) {
			data := readSettingsInput(t, c.input)
			got, reformatted, err := EditSettings(data, c.ops)
			if err != nil {
				t.Fatal(err)
			}
			if reformatted != c.reformatted {
				t.Errorf("reformatted %v, want %v:\n%s", reformatted, c.reformatted, got)
			}
			if _, err := Parse(File, got); err != nil {
				t.Errorf("result does not load: %v\n%s", err, got)
			}
			settingsGolden(t, strings.TrimSuffix(c.input, ".yaml")+"."+c.name+".golden.yaml", got)
		})
	}
	// Keys outside the form and every comment of an entry not removed
	// survive the splice byte for byte.
	data := readSettingsInput(t, "commented.yaml")
	got, _, err := EditSettings(data, []SettingOp{{Key: "ui.labels", Value: "plain"}, {Key: "tests.exclude", Value: []string{"x"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, keep := range []string{"# Team settings for Graphvein.", "repository: shop   # shared ID", "# Labels: the team reads ids.", "# decided 2026-10",
		"    journal_runs: 30   # kept, not in the form", "        - key: nextjs_base_path:web\n          value: /dashboard\n", "# scenario tests"} {
		if !strings.Contains(string(got), keep) {
			t.Errorf("lost %q:\n%s", keep, got)
		}
	}
	// The re-encoded fallback keeps the comments of what it keeps.
	got, _, _ = EditSettings(readSettingsInput(t, "listcomments.yaml"), []SettingOp{{Key: "tests.exclude", Value: []string{"x"}}})
	if !strings.Contains(string(got), "# the scenario suite") {
		t.Errorf("fallback lost a comment:\n%s", got)
	}
}

// TestSettingsEditEntries checks row ST-27 for block lists and mappings:
// an edit changes them entry by entry, so every line of an entry it keeps
// stays (head and line comments included), a mapping entry whose value
// alone changes keeps its line comment, and only a removed entry's own
// comments are lost; a reordered mapping falls back to the re-encode,
// whose lost comments are found by comparing both texts.
func TestSettingsEditEntries(t *testing.T) {
	base := []MapEntry{{"contracts", "/api"}, {"contracts/admin.yaml", "/admin/api"}, {"legacy", "/old"}}
	cases := []struct {
		name        string
		ops         []SettingOp
		reformatted bool
		lost        []Comment
	}{
		{"map-add", []SettingOp{{Key: "openapi.base_paths", Value: append(append([]MapEntry{}, base...), MapEntry{"contracts/v2", "/v2"})}}, false, nil},
		{"map-add-first", []SettingOp{{Key: "openapi.base_paths", Value: append([]MapEntry{{"contracts/v0", "/v0"}}, base...)}}, false, nil},
		{"map-remove", []SettingOp{{Key: "openapi.base_paths", Value: base[:2]}}, false, []Comment{{7, "# retire in 2027"}}},
		{"map-remove-first", []SettingOp{{Key: "openapi.base_paths", Value: base[1:]}}, false, []Comment{{4, "# the public API"}, {5, "# mounted by the gateway"}}},
		{"map-change", []SettingOp{{Key: "openapi.base_paths", Value: []MapEntry{{"contracts", "/api/v1"}, base[1], base[2]}}}, false, nil},
		{"map-change-add-remove", []SettingOp{{Key: "openapi.base_paths", Value: []MapEntry{{"contracts", "/api/v1"}, {"contracts/v2", "/v2"}, base[2]}}}, false, []Comment{{6, "# admin only"}}},
		{"map-reorder", []SettingOp{{Key: "openapi.base_paths", Value: []MapEntry{base[2], base[0], base[1]}}}, true, nil},
		{"list-add", []SettingOp{{Key: "openapi.include", Value: []string{"contracts/**", "gen/**", "more/**", "vendor/specs/**"}}}, false, nil},
		{"list-remove", []SettingOp{{Key: "openapi.include", Value: []string{"contracts/**", "vendor/specs/**"}}}, false, []Comment{{10, "# generated"}}},
	}
	data := readSettingsInput(t, "mapcomments.yaml")
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, reformatted, err := EditSettings(data, c.ops)
			if err != nil {
				t.Fatal(err)
			}
			if reformatted != c.reformatted {
				t.Errorf("reformatted %v, want %v:\n%s", reformatted, c.reformatted, got)
			}
			if _, err := Parse(File, got); err != nil {
				t.Errorf("result does not load: %v\n%s", err, got)
			}
			ref, err := editedTree(data, c.ops[0])
			if err != nil || !sameTree(got, ref) {
				t.Errorf("result differs from the node edit (%v):\n%s", err, got)
			}
			lost := LostComments(data, got)
			if len(lost) != len(c.lost) || len(lost) > 0 && !reflect.DeepEqual(lost, c.lost) {
				t.Errorf("lost comments %+v, want %+v", lost, c.lost)
			}
			settingsGolden(t, "mapcomments."+c.name+".golden.yaml", got)
		})
	}
}

// TestComments checks the comment scanner LostComments uses: comments
// after a blank or at a line start, not a # inside a word, a quoted
// scalar or a block scalar's lines.
func TestComments(t *testing.T) {
	text := "a: x#y   # one\n# two\nb: 'it''s # not' # three\nc: \"q \\\" # no\"\nd: |\n  # body\n  text\ne: don't # four\n- >-\n    # body\n"
	var got []string
	for _, c := range Comments([]byte(text)) {
		got = append(got, c.Text)
	}
	want := []string{"# one", "# two", "# three", "# four"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("comments %q, want %q", got, want)
	}
	lost := LostComments([]byte("a: 1 # x\nb: 2 # x\nc: 3 # y\n"), []byte("b: 2 # x\n"))
	if !reflect.DeepEqual(lost, []Comment{{2, "# x"}, {3, "# y"}}) && !reflect.DeepEqual(lost, []Comment{{1, "# x"}, {3, "# y"}}) {
		t.Errorf("lost %+v", lost)
	}
}

// TestSettingsEditCRLF checks that a file with CRLF line endings keeps
// them on the lines an edit writes (row ST-27). The text is built here:
// the repository normalises committed files to LF.
func TestSettingsEditCRLF(t *testing.T) {
	data := []byte("version: 1\r\nui:\r\n  labels: plain # crlf\r\n")
	got, reformatted, err := EditSettings(data, []SettingOp{{Key: "ui.labels", Value: "technical"}, {Key: "index.shape_depth", Value: 3}})
	want := "version: 1\r\nui:\r\n  labels: technical # crlf\r\nindex:\r\n  shape_depth: 3\r\n"
	if err != nil || reformatted || string(got) != want {
		t.Fatalf("%q %v %v, want %q", got, reformatted, err, want)
	}
}

// TestSettingsEditMissingFile checks that a first edit creates the file
// with version 1 and the key, and that removing it again leaves only the
// version.
func TestSettingsEditMissingFile(t *testing.T) {
	got, reformatted, err := EditSettings(nil, []SettingOp{{Key: "ui.labels", Value: "technical"}})
	if err != nil || reformatted || string(got) != "version: 1\nui:\n  labels: technical\n" {
		t.Fatalf("%q %v %v", got, reformatted, err)
	}
	got, _, err = EditSettings(got, []SettingOp{{Key: "ui.labels", Remove: true}})
	if err != nil || string(got) != "version: 1\n" {
		t.Fatalf("%q %v", got, err)
	}
	// A comment that opens the file is its header, not the head comment
	// of the first key: removing that key keeps it.
	got, _, err = EditSettings([]byte("# header\nui:\n  labels: plain\nversion: 1\n"), []SettingOp{{Key: "ui.labels", Remove: true}})
	if err != nil || string(got) != "# header\nversion: 1\n" {
		t.Fatalf("%q %v", got, err)
	}
}

// TestSettingsEditRefusals checks row ST-26 at the writer: a key outside
// the table, a value of the wrong type, removing a key the file does not
// set and editing ide.editor under a link_template are refused.
func TestSettingsEditRefusals(t *testing.T) {
	for _, c := range []struct {
		data string
		op   SettingOp
		want string
	}{
		{"version: 1\n", SettingOp{Key: "changes.max_pins", Value: 3}, "not a key the Settings form edits"},
		{"version: 1\n", SettingOp{Key: "index.shape_depth", Value: "6"}, "takes a integer value"},
		{"version: 1\n", SettingOp{Key: "ui.labels", Remove: true}, "does not set ui.labels"},
		{"version: 1\nide:\n  link_template: 'subl://{abs_path}'\n", SettingOp{Key: "ide.editor", Value: "zed"}, "ide.link_template is set"},
	} {
		if _, _, err := EditSettings([]byte(c.data), []SettingOp{c.op}); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: %v, want %q", c.op, err, c.want)
		}
	}
}

// TestSettingValues checks rows ST-12..ST-14 and ST-16 at the reader:
// each form key with its value, origin and line, and the keys outside the
// form in file order.
func TestSettingValues(t *testing.T) {
	data := readSettingsInput(t, "commented.yaml")
	if _, err := Parse(File, data); err != nil {
		t.Fatal(err)
	}
	values, other, err := SettingValues(data)
	if err != nil {
		t.Fatal(err)
	}
	type got struct {
		Value  any
		Origin string
		Line   int
	}
	want := map[string]got{
		"ui.labels":                     {"technical", OriginFile, 7},
		"ide.editor":                    {"cursor", OriginFile, 9},
		"index.shape_depth":             {6, OriginFile, 12},
		"clients.base_paths":            {nil, OriginNotSet, 0},
		"clients.link_generated_output": {false, OriginBuiltin, 0},
		"tests.include":                 {[]string{"checks/**"}, OriginFile, 15},
		"tests.exclude":                 {[]string{"tests/production-adapter.ts"}, OriginFile, 16},
		"openapi.include":               {nil, OriginNotSet, 0},
		"openapi.exclude":               {nil, OriginNotSet, 0},
		"openapi.base_paths":            {nil, OriginNotSet, 0},
		"architecture.hide":             {nil, OriginNotSet, 0},
		"ui.impact_depth":               {DefaultImpactDepth, OriginBuiltin, 0},
		"ui.chain_depth":                {DefaultChainDepth, OriginBuiltin, 0},
	}
	if len(values) != len(settingKeys) {
		t.Fatalf("%d values for %d keys", len(values), len(settingKeys))
	}
	for _, v := range values {
		if g := (got{v.Value, v.Origin, v.Line}); !reflect.DeepEqual(g, want[v.Key.Key]) {
			t.Errorf("%s: %+v, want %+v", v.Key.Key, g, want[v.Key.Key])
		}
	}
	wantOther := []OtherKey{{"version", 2}, {"repository", 3}, {"changes.journal_runs", 20}, {"explorer.path_values", 23}}
	if !reflect.DeepEqual(other, wantOther) {
		t.Errorf("other keys %+v, want %+v", other, wantOther)
	}
	// No file: built-in defaults and nothing set.
	values, other, _ = SettingValues(nil)
	if values[0].Origin != OriginBuiltin || values[0].Value != LabelsPlain || values[3].Origin != OriginNotSet || other != nil {
		t.Errorf("no file: %+v %+v", values[0], other)
	}
	// A mapping ide with a link_template locks ide.editor.
	values, _, _ = SettingValues([]byte("version: 1\nide:\n  editor: zed\n  link_template: 'x://{abs_path}'\n"))
	if values[1].Locked != "ide.link_template" || values[1].Value != "zed" || values[1].Line != 3 {
		t.Errorf("locked editor: %+v", values[1])
	}
	values, _, _ = SettingValues([]byte("version: 1\nclients:\n  base_paths:\n    web: /api\n    .: ''\n"))
	if !reflect.DeepEqual(values[3].Value, []MapEntry{{"web", "/api"}, {".", ""}}) {
		t.Errorf("map value: %+v", values[3].Value)
	}
}

// TestSettingDefaults keeps the table's built-in defaults equal to the
// decoders' (rows ST-1..ST-3, ST-5).
func TestSettingDefaults(t *testing.T) {
	d := Defaults()
	for _, k := range settingKeys {
		var want any
		switch k.Key {
		case "ui.labels":
			want = LabelsPlain // decodeUI leaves "" and the UI applies plain (ADR 0078)
		case "ide.editor":
			want = d.IDE.Editor
		case "index.shape_depth":
			want = d.Index.ShapeDepth
		case "clients.link_generated_output":
			want = d.Clients.LinkGeneratedOutput
		case "ui.impact_depth":
			want = DefaultImpactDepth // decodeUI leaves 0 and the UI applies the built-in depth (T042)
		case "ui.chain_depth":
			want = DefaultChainDepth
		}
		if k.Default != want {
			t.Errorf("%s default %v, want %v", k.Key, k.Default, want)
		}
		if k.Applies != AppliesAtOnce && k.Applies != AppliesNextIndexRun {
			t.Errorf("%s applies %q", k.Key, k.Applies)
		}
	}
}

// TestSettingsEditMatchesTree checks row ST-27's guarantee on every key:
// whether spliced or re-encoded, a set and then a remove of each key on
// each input gives the tree the node edit gives, and a file that loads.
func TestSettingsEditMatchesTree(t *testing.T) {
	samples := map[string]any{
		SettingEnum: nil, SettingInteger: 7, SettingBoolean: true,
		SettingList: []string{"a/**", "b"}, SettingMap: []MapEntry{{"web", "/api"}},
	}
	inputs := [][]byte{nil, []byte("version: 1\n"), []byte("# h\nversion: 1\nide:\n  editor: zed\n  label: Zed  # mine\n")}
	for _, name := range []string{"commented.yaml", "listcomments.yaml", "flow.yaml"} {
		inputs = append(inputs, readSettingsInput(t, name))
	}
	for i, data := range inputs {
		for _, k := range settingKeys {
			v := samples[k.Kind]
			if v == nil {
				v = k.Choices[len(k.Choices)-1]
			}
			text := data
			for _, op := range []SettingOp{{Key: k.Key, Value: v}, {Key: k.Key, Remove: true}} {
				got, _, err := EditSettings(text, []SettingOp{op})
				if err != nil {
					t.Fatalf("input %d %+v: %v", i, op, err)
				}
				base := text
				if len(base) == 0 {
					base = []byte("version: 1\n")
				}
				ref, err := editedTree(base, op)
				if err != nil {
					t.Fatal(err)
				}
				if !sameTree(got, ref) {
					t.Errorf("input %d %+v: result differs from the node edit:\n%s", i, op, got)
				}
				if _, err := Parse(File, got); err != nil {
					t.Errorf("input %d %+v: %v\n%s", i, op, err, got)
				}
				text = got
			}
		}
	}
}

// TestDecodeNamesLineOnce checks that a section decoder's message, which
// already names its line, is not given the section's line in front again
// (the preview and the Settings page show it as it is).
func TestDecodeNamesLineOnce(t *testing.T) {
	_, err := Parse(File, []byte("version: 1\nindex:\n  shape_depth: 99\n"))
	if err == nil || err.Error() != "graphvein.yaml: line 3: index.shape_depth must be an integer from 1 to 10" {
		t.Errorf("message %v", err)
	}
	_, err = Parse(File, []byte("version: 1\nui: 3\n"))
	if err == nil || strings.Count(err.Error(), "line ") != 1 {
		t.Errorf("message %v", err)
	}
}

// TestResolveServeRepository checks the identity graphvein serve uses
// when graphvein.yaml does not load: the file's own repository key, the
// directory name only when the file sets none, the override, and an
// error rather than a guess when the key is there but cannot be read.
func TestResolveServeRepository(t *testing.T) {
	override := "given"
	for _, c := range []struct {
		text     string
		override *string
		want     string
		loads    bool
		fails    bool
	}{
		{"version: 1\nrepository: shop\n", nil, "shop", true, false},
		{"version: 1\nrepository: shop\nindex:\n  shape_depth: 99\n", nil, "shop", false, false},
		{"version: 1\nrepository: shop # id\nindex: [\n", nil, "shop", false, false},
		{"version: 1\nindex: [\n", nil, "repo", false, false},
		{"version: 1\nindex:\n  shape_depth: 99\n", nil, "repo", false, false},
		{"version: 1\nrepository: [shop\n", nil, "", false, true},
		{"version: 1\nrepository: 3\nui: 1\n", nil, "", false, true},
		{"version: 1\nrepository: [shop\n", &override, "given", false, false},
	} {
		root := filepath.Join(t.TempDir(), "repo")
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, File), []byte(c.text), 0o644); err != nil {
			t.Fatal(err)
		}
		id, loadErr, err := ResolveServeRepository(root, c.override)
		if id != c.want || (loadErr == nil) != c.loads || (err != nil) != c.fails {
			t.Errorf("%q: %q %v %v", c.text, id, loadErr, err)
		}
	}
}

// TestResolveRegisteredRepository checks the identity serve uses for a
// workspace entry: the file's repository key, loading or not, else the
// registered name; a file that does not load is reported, not refused.
func TestResolveRegisteredRepository(t *testing.T) {
	for _, c := range []struct {
		text  string // "" means no graphvein.yaml
		want  string
		loads bool
	}{
		{"", "registered", true},
		{"version: 1\n", "registered", true},
		{"version: 1\nrepository: shop\n", "shop", true},
		{"version: 1\nrepository: shop\nindex:\n  shape_depth: 99\n", "shop", false},
		{"version: 1\nindex: [\n", "registered", false},
		{"version: 1\nrepository: [shop\n", "registered", false},
	} {
		root := t.TempDir()
		if c.text != "" {
			if err := os.WriteFile(filepath.Join(root, File), []byte(c.text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		id, loadErr, err := ResolveRegisteredRepository(root, "registered")
		if err != nil || id != c.want || (loadErr == nil) != c.loads {
			t.Errorf("%q: %q %v %v", c.text, id, loadErr, err)
		}
	}
}

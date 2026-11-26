package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// indexInputBase is a file setting every index input, with comments, block
// and flow styles and keys outside the inputs.
const indexInputBase = `# Team settings
version: 1
repository: shop   # shared ID
ui:
  labels: technical
ide: cursor
index:
  shape_depth: 4   # deeper is slower
tests:
  include: ['checks/**', 'e2e/**']
  exclude:
    - tests/production-adapter.ts
openapi:
  include: [api/**]
  exclude: [api/old/**]
  base_paths:
    api/a.yaml: /v1
    api/b.yaml: /v2
clients:
  base_paths:
    web: /app
  link_generated_output: true
architecture:
  hide: [scripts/]
changes:
  journal_runs: 30
`

func mustInputs(t *testing.T, text string) IndexInputs {
	t.Helper()
	if _, err := Parse(File, []byte(text)); err != nil {
		t.Fatalf("fixture does not load: %v\n%s", err, text)
	}
	in, err := IndexInputsOf([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return in
}

// TestIndexInputKeys checks that the index inputs are exactly the key
// rows that apply at the next index run (ST-3..ST-10), and that no
// at-once key is one (ST-45).
func TestIndexInputKeys(t *testing.T) {
	want := []string{"index.shape_depth", "clients.base_paths", "clients.link_generated_output", "tests.include", "tests.exclude", "openapi.include", "openapi.exclude", "openapi.base_paths"}
	if got := IndexInputKeys(); !reflect.DeepEqual(got, want) {
		t.Fatalf("index input keys = %v, want %v", got, want)
	}
	in := mustInputs(t, indexInputBase)
	for _, k := range settingKeys {
		_, ok := in.values[k.Key]
		if ok != (k.Applies == AppliesNextIndexRun) {
			t.Errorf("%s (%s) recorded = %v", k.Key, k.Applies, ok)
		}
	}
}

// TestIndexInputsCanonical checks row ST-42 and ST-45: edits that change
// nothing the index reads keep the hash and compare as unchanged.
func TestIndexInputsCanonical(t *testing.T) {
	base := mustInputs(t, indexInputBase)
	same := map[string]string{
		"comments and blank lines":  strings.ReplaceAll(strings.ReplaceAll(indexInputBase, "# Team settings", "# Edited header\n\n# more"), "   # deeper is slower", ""),
		"list order and duplicates": strings.Replace(indexInputBase, "['checks/**', 'e2e/**']", "['e2e/**', \"checks/**\", 'e2e/**']", 1),
		"map order":                 strings.Replace(indexInputBase, "    api/a.yaml: /v1\n    api/b.yaml: /v2\n", "    api/b.yaml: /v2\n    api/a.yaml: /v1\n", 1),
		"block instead of flow":     strings.Replace(indexInputBase, "  include: [api/**]\n", "  include:\n    - api/**\n", 1),
		"four-space indentation":    strings.ReplaceAll(indexInputBase, "\n  ", "\n    "),
		"ui.labels (at once)":       strings.Replace(indexInputBase, "labels: technical", "labels: plain", 1),
		"ide.editor (at once)":      strings.Replace(indexInputBase, "ide: cursor", "ide: zed", 1),
		"architecture.hide":         strings.Replace(indexInputBase, "hide: [scripts/]", "hide: [scripts/, tools/]", 1),
		"changes.journal_runs":      strings.Replace(indexInputBase, "journal_runs: 30", "journal_runs: 5", 1),
		"repository":                strings.Replace(indexInputBase, "repository: shop", "repository: other", 1),
	}
	for name, text := range same {
		got := mustInputs(t, text)
		if got.Hash() != base.Hash() {
			t.Errorf("%s changed the hash", name)
		}
		if c := CompareIndexInputs(base.Record(), got); c.State != IndexSettingsUnchanged || len(c.ChangedKeys) != 0 {
			t.Errorf("%s: %+v", name, c)
		}
	}
	// Writing a default explicitly, or an empty list, is the same as not
	// setting the key.
	defaults := mustInputs(t, "version: 1\n")
	explicit := mustInputs(t, "version: 1\nindex:\n  shape_depth: 6\ntests:\n  include: []\nclients:\n  link_generated_output: false\n")
	if defaults.Hash() != explicit.Hash() {
		t.Error("explicit defaults changed the hash")
	}
	none, err := IndexInputsOf(nil)
	if err != nil || none.Hash() != defaults.Hash() {
		t.Errorf("no file = %v %v", none.Hash(), err)
	}
}

// TestIndexInputsChanged checks row ST-43: every input key's change is
// named, in key-table order, with both hashes.
func TestIndexInputsChanged(t *testing.T) {
	base := mustInputs(t, indexInputBase)
	cases := map[string]struct{ text, key string }{
		"depth":     {strings.Replace(indexInputBase, "shape_depth: 4", "shape_depth: 5", 1), "index.shape_depth"},
		"clients":   {strings.Replace(indexInputBase, "web: /app", "web: /web", 1), "clients.base_paths"},
		"generated": {strings.Replace(indexInputBase, "link_generated_output: true", "link_generated_output: false", 1), "clients.link_generated_output"},
		"tests in":  {strings.Replace(indexInputBase, "'e2e/**'", "'it/**'", 1), "tests.include"},
		"tests ex":  {strings.Replace(indexInputBase, "    - tests/production-adapter.ts\n", "    - tests/other.ts\n", 1), "tests.exclude"},
		"api in":    {strings.Replace(indexInputBase, "include: [api/**]", "include: [spec/**]", 1), "openapi.include"},
		"api ex":    {strings.Replace(indexInputBase, "exclude: [api/old/**]", "exclude: []", 1), "openapi.exclude"},
		"api bases": {strings.Replace(indexInputBase, "api/b.yaml: /v2", "api/b.yaml: /v3", 1), "openapi.base_paths"},
	}
	for name, tc := range cases {
		got := mustInputs(t, tc.text)
		c := CompareIndexInputs(base.Record(), got)
		if c.State != IndexSettingsChanged || !reflect.DeepEqual(c.ChangedKeys, []string{tc.key}) || c.IndexHash != base.Hash() || c.CurrentHash != got.Hash() || c.IndexHash == c.CurrentHash {
			t.Errorf("%s: %+v", name, c)
		}
	}
	two := strings.Replace(strings.Replace(indexInputBase, "web: /app", "web: /web", 1), "shape_depth: 4", "shape_depth: 9", 1)
	if c := CompareIndexInputs(base.Record(), mustInputs(t, two)); !reflect.DeepEqual(c.ChangedKeys, []string{"index.shape_depth", "clients.base_paths"}) {
		t.Errorf("two keys: %v", c.ChangedKeys)
	}
	// The flag's shape depth is what the run used.
	flag := base.WithShapeDepth(8)
	if c := CompareIndexInputs(flag.Record(), base); !reflect.DeepEqual(c.ChangedKeys, []string{"index.shape_depth"}) {
		t.Errorf("flag depth: %+v", c)
	}
	if base.values["index.shape_depth"] != 4 {
		t.Error("WithShapeDepth changed its receiver")
	}
}

// TestIndexInputsUnknown checks row ST-44: nothing is reported unchanged
// without a record that proves it.
func TestIndexInputsUnknown(t *testing.T) {
	base := mustInputs(t, indexInputBase)
	for name, record := range map[string]string{
		"no record":    "",
		"not JSON":     "{",
		"other format": `{"format":2,"hash":"sha256:x","values":{}}`,
	} {
		c := CompareIndexInputs(record, base)
		if c.State != IndexSettingsUnknown || c.Reason == "" || len(c.ChangedKeys) != 0 {
			t.Errorf("%s: %+v", name, c)
		}
	}
	// A key the record lacks: unknown, unless another key changed.
	missing := strings.Replace(base.Record(), `"tests.exclude":["tests/production-adapter.ts"],`, "", 1)
	if missing == base.Record() {
		t.Fatalf("record layout changed: %s", base.Record())
	}
	if c := CompareIndexInputs(missing, base); c.State != IndexSettingsUnknown || !reflect.DeepEqual(c.UnknownKeys, []string{"tests.exclude"}) {
		t.Errorf("missing key: %+v", c)
	}
	changed := mustInputs(t, strings.Replace(indexInputBase, "shape_depth: 4", "shape_depth: 5", 1))
	if c := CompareIndexInputs(missing, changed); c.State != IndexSettingsChanged || !reflect.DeepEqual(c.ChangedKeys, []string{"index.shape_depth"}) || !reflect.DeepEqual(c.UnknownKeys, []string{"tests.exclude"}) {
		t.Errorf("missing key and a change: %+v", c)
	}
	// A current file that does not load: unknown with the decoder's
	// message and the file name only.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, File), []byte("version: 1\nindex:\n  shape_depth: 99\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := IndexSettingsAt(base.Record(), root)
	if c.State != IndexSettingsUnknown || !strings.HasPrefix(c.Reason, "the current graphvein.yaml does not load: graphvein.yaml: line 3") || strings.Contains(c.Reason, root) || c.IndexHash != base.Hash() || c.CurrentHash != "" {
		t.Errorf("file does not load: %+v", c)
	}
	if err := os.WriteFile(filepath.Join(root, File), []byte(indexInputBase), 0o644); err != nil {
		t.Fatal(err)
	}
	if c := IndexSettingsAt(base.Record(), root); c.State != IndexSettingsUnchanged {
		t.Errorf("file at root: %+v", c)
	}
}

// TestLoadIndexInputs checks that a run's inputs come from the bytes it
// decoded, and that a missing file gives the defaults.
func TestLoadIndexInputs(t *testing.T) {
	root := t.TempDir()
	c, in, err := LoadIndexInputs(root)
	if err != nil || c.Index.ShapeDepth != DefaultShapeDepth || in.Hash() != mustInputs(t, "version: 1\n").Hash() {
		t.Fatalf("no file: %+v %v %v", c.Index, in.Hash(), err)
	}
	if err := os.WriteFile(filepath.Join(root, File), []byte(indexInputBase), 0o644); err != nil {
		t.Fatal(err)
	}
	c, in, err = LoadIndexInputs(root)
	if err != nil || c.Index.ShapeDepth != 4 || in.Hash() != mustInputs(t, indexInputBase).Hash() {
		t.Fatalf("file: %+v %v %v", c.Index, in.Hash(), err)
	}
	if err := os.WriteFile(filepath.Join(root, File), []byte("version: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, in, err := LoadIndexInputs(root); err == nil || !in.IsZero() {
		t.Fatalf("invalid file: %v", err)
	}
}

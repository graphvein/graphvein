package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// TestUILabels checks ui.labels (T025b): absent is "", the two styles are
// read, and every other form fails to load with a message naming the key.
func TestUILabels(t *testing.T) {
	root := t.TempDir()
	if c, err := Load(root); err != nil || c.UI.Labels != "" {
		t.Fatalf("no file: %+v %v", c.UI, err)
	}
	write := func(input string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte(input), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ input, want string }{
		{"version: 1\n", ""},
		{"version: 1\nui: {}\n", ""},
		{"version: 1\nui: {labels: plain}\n", LabelsPlain},
		{"version: 1\nui:\n  labels: technical\n", LabelsTechnical},
	} {
		write(tc.input)
		c, err := Load(root)
		if err != nil || c.UI.Labels != tc.want {
			t.Errorf("%q: labels %q, err %v; want %q", tc.input, c.UI.Labels, err, tc.want)
		}
	}
	for _, tc := range []struct{ input, msg string }{
		{"version: 1\nui: technical\n", "ui must be a mapping"},
		{"version: 1\nui: {labels: Technical}\n", "ui.labels must be plain or technical"},
		{"version: 1\nui: {labels: ''}\n", "ui.labels must be plain or technical"},
		{"version: 1\nui: {labels: 1}\n", "ui.labels must be plain or technical"},
		{"version: 1\nui: {labels: [plain]}\n", "ui.labels must be plain or technical"},
		{"version: 1\nui: {labels: plain, labels: technical}\n", "duplicate ui key labels"},
		{"version: 1\nui: {theme: dark}\n", "unknown ui key theme"},
		{"version: 1\nui: {}\nui: {}\n", "duplicate key ui"},
	} {
		write(tc.input)
		if _, err := Load(root); err == nil || !strings.Contains(err.Error(), tc.msg) {
			t.Errorf("%q: err %v, want %q", tc.input, err, tc.msg)
		}
	}
}

// TestUIDepths checks ui.impact_depth and ui.chain_depth (T042): absent is
// 0 (the UI's built-in default applies), 1 to 10 are read, and every other
// form fails to load with a message naming the key and the range.
func TestUIDepths(t *testing.T) {
	root := t.TempDir()
	write := func(input string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte(input), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		input         string
		impact, chain int
	}{
		{"version: 1\nui: {}\n", 0, 0},
		{"version: 1\nui: {impact_depth: 1}\n", 1, 0},
		{"version: 1\nui: {chain_depth: 10}\n", 0, 10},
		{"version: 1\nui:\n  labels: technical\n  impact_depth: 4\n  chain_depth: 8\n", 4, 8},
	} {
		write(tc.input)
		c, err := Load(root)
		if err != nil || c.UI.ImpactDepth != tc.impact || c.UI.ChainDepth != tc.chain {
			t.Errorf("%q: %+v, err %v; want impact %d chain %d", tc.input, c.UI, err, tc.impact, tc.chain)
		}
	}
	for _, tc := range []struct{ input, msg string }{
		{"version: 1\nui: {impact_depth: 0}\n", "line 2: ui.impact_depth must be an integer from 1 to 10"},
		{"version: 1\nui: {impact_depth: 11}\n", "ui.impact_depth must be an integer from 1 to 10"},
		{"version: 1\nui: {impact_depth: '4'}\n", "ui.impact_depth must be an integer from 1 to 10"},
		{"version: 1\nui: {impact_depth: 4.0}\n", "ui.impact_depth must be an integer from 1 to 10"},
		{"version: 1\nui: {chain_depth: -1}\n", "ui.chain_depth must be an integer from 1 to 10"},
		{"version: 1\nui: {chain_depth: [6]}\n", "ui.chain_depth must be an integer from 1 to 10"},
		{"version: 1\nui: {chain_depth: 99999999999999999999}\n", "ui.chain_depth must be an integer from 1 to 10"},
		{"version: 1\nui: {chain_depth: 6, chain_depth: 7}\n", "duplicate ui key chain_depth"},
		{"version: 1\nui: {depth: 6}\n", "unknown ui key depth"},
	} {
		write(tc.input)
		if _, err := Load(root); err == nil || !strings.Contains(err.Error(), tc.msg) {
			t.Errorf("%q: err %v, want %q", tc.input, err, tc.msg)
		}
	}
}

// TestUIKeysTable checks the one table of ui keys: names are unique, a
// word key's default is one of its words, a number key's default lies in
// its range, and every row's default decodes to a set value.
func TestUIKeysTable(t *testing.T) {
	names := map[string]bool{}
	for _, k := range UIKeys {
		if names[k.Name] {
			t.Errorf("duplicate row %s", k.Name)
		}
		names[k.Name] = true
		if k.Doc == "" || k.set == nil {
			t.Errorf("%s: missing doc or setter", k.Name)
		}
		var node yaml.Node
		if err := yaml.Unmarshal([]byte("ui: {"+k.Name+": "+k.Default+"}"), &node); err != nil {
			t.Fatal(err)
		}
		u, err := decodeUI(node.Content[0].Content[1])
		if err != nil || u == (UI{}) {
			t.Errorf("%s: default %q does not decode: %+v %v", k.Name, k.Default, u, err)
		}
		if k.Words == nil && (k.Min != MinUIDepth || k.Max != MaxUIDepth) {
			t.Errorf("%s: range %d..%d", k.Name, k.Min, k.Max)
		}
	}
	if len(names) != 3 || !names["labels"] || !names["impact_depth"] || !names["chain_depth"] {
		t.Errorf("rows %v", names)
	}
}

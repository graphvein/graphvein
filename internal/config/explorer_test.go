package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExplorerSection decodes explorer.path_values and refuses every
// malformed form (rows OG-90, OG-91 of
// docs/contract/claims/openapi-generation.md).
func TestExplorerSection(t *testing.T) {
	root := t.TempDir()
	write := func(text string) (Config, error) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, File), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return Load(root)
	}
	c, err := write("version: 1\nexplorer:\n  path_values:\n    - key: \"route:app.py#GET%20%2Fx\"\n      value: /api/x/{id}\n    - key: nextjs_base_path:web\n      value: /web\n    - key: nextjs_base_path:.\n      value: \"\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Explorer.PathValues) != 3 || c.Explorer.PathValues[0].Value != "/api/x/{id}" || c.Explorer.PathValues[1].Line != 6 {
		t.Fatalf("%+v", c.Explorer)
	}
	if v, ok := c.Explorer.PathValueFor("nextjs_base_path:."); !ok || v != "" {
		t.Error("PathValueFor")
	}
	if c, err := write("version: 1\n"); err != nil || len(c.Explorer.PathValues) != 0 {
		t.Errorf("no section: %v", err)
	}
	for _, bad := range []struct{ text, want string }{
		{"explorer: []", "explorer must be a mapping"},
		{"explorer: {other: 1}", "unknown explorer key other"},
		{"explorer: {path_values: {}}", "must be a list"},
		{"explorer: {path_values: [x]}", "is a mapping with key and value"},
		{"explorer: {path_values: [{key: \"route:a#b\"}]}", "needs key and value"},
		{"explorer: {path_values: [{key: \"route:a#b\", value: /x, note: y}]}", "unknown explorer.path_values key note"},
		{"explorer: {path_values: [{key: \"route:a#b\", value: 1}]}", "value must be a string"},
		{"explorer: {path_values: [{key: other:a, value: /x}]}", "must be a route identity"},
		{"explorer: {path_values: [{key: \"route:\", value: /x}]}", "must be a route identity"},
		{"explorer: {path_values: [{key: \"route:a b\", value: /x}]}", "must be a route identity"},
		{"explorer: {path_values: [{key: \"route:a#b\", value: x}]}", "must start with /"},
		{"explorer: {path_values: [{key: \"route:a#b\", value: \"/x?y\"}]}", "must not hold"},
		{"explorer: {path_values: [{key: \"route:a#b\", value: \"/x/{a/b}\"}]}", "must be a name"},
		{"explorer: {path_values: [{key: \"route:a#b\", value: \"/x/{a\"}]}", "without its }"},
		{"explorer: {path_values: [{key: \"route:a#b\", value: \"/x/a}\"}]}", "} without {"},
		{"explorer: {path_values: [{key: \"route:a#b\", value: \"/x/{*a}\"}]}", "must be a name"},
		{"explorer: {path_values: [{key: nextjs_base_path:web, value: /web/}]}", "does not end with /"},
		{"explorer: {path_values: [{key: nextjs_base_path:web, value: \"/{x}\"}]}", "must not hold { or }"},
		{"explorer: {path_values: [{key: \"route:a#b\", value: /x}, {key: \"route:a#b\", value: /y}]}", "already set on line"},
		{"explorer: {path_values: [{key: \"route:a#b\", value: /x, registration: abc}]}", "registration must be a sha256"},
		{"explorer: {path_values: [{key: nextjs_base_path:web, value: /x, registration: \"sha256:" + strings.Repeat("0", 64) + "\"}]}", "belongs to a route or mount key only"},
	} {
		if _, err := write("version: 1\n" + bad.text + "\n"); err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Errorf("%s: %v, want %q", bad.text, err, bad.want)
		}
	}
}

// TestExplorerWriter sets, replaces and removes path values, keeping the
// rest of the file and its comments, and creates a file from nothing.
func TestExplorerWriter(t *testing.T) {
	original := []byte("# team settings\nversion: 1\nrepository: app # the id\n")
	next, err := SetExplorerPathValue(original, "route:a#b", "/x", "")
	if err != nil {
		t.Fatal(err)
	}
	next, err = SetExplorerPathValue(next, "nextjs_base_path:web", "/web", "")
	if err != nil {
		t.Fatal(err)
	}
	next, err = SetExplorerPathValue(next, "route:a#b", "/y/{id}", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := Parse(File, next)
	if err != nil || len(c.Explorer.PathValues) != 2 || c.Explorer.PathValues[0].Value != "/y/{id}" || c.Repository != "app" {
		t.Fatalf("%v %+v\n%s", err, c.Explorer, next)
	}
	if !strings.Contains(string(next), "# team settings") || !strings.Contains(string(next), "# the id") {
		t.Errorf("comments lost:\n%s", next)
	}
	for _, key := range []string{"route:a#b", "nextjs_base_path:web"} {
		if next, err = RemoveExplorerPathValue(next, key); err != nil {
			t.Fatal(err)
		}
	}
	if string(next) != string(original) {
		t.Errorf("set then remove changed the file:\n%s", next)
	}
	// A registration is written, replaced and dropped with the value.
	h := "sha256:" + strings.Repeat("a", 64)
	reg, err := SetExplorerPathValue(original, "route:a#b%402", "/x", h)
	if err != nil || !strings.Contains(string(reg), "registration: "+h) {
		t.Fatalf("%v\n%s", err, reg)
	}
	if c, err := Parse(File, reg); err != nil || c.Explorer.PathValues[0].Registration != h {
		t.Errorf("registration not read back: %v", err)
	}
	if reg, err = SetExplorerPathValue(reg, "route:a#b%402", "/y", ""); err != nil || strings.Contains(string(reg), "registration") {
		t.Errorf("registration not dropped: %v\n%s", err, reg)
	}
	if _, err := RemoveExplorerPathValue(original, "route:a#b"); err == nil {
		t.Error("removing a missing value succeeded")
	}
	created, err := SetExplorerPathValue(nil, "route:a#b", "/x", "")
	if err != nil || string(created) != "version: 1\nexplorer:\n  path_values:\n    - key: route:a#b\n      value: /x\n" {
		t.Errorf("%v %q", err, created)
	}
	if _, err := SetExplorerPathValue([]byte("version: 1\nexplorer: [x]\n"), "route:a#b", "/x", ""); err == nil {
		t.Error("explorer list accepted")
	}
	if _, err := SetExplorerPathValue([]byte("- a\n"), "route:a#b", "/x", ""); err == nil {
		t.Error("a list file accepted")
	}
}

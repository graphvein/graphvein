package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// TestOpenAPICommand indexes a small Express app and checks graphvein
// openapi: JSON on standard output, the same document as YAML in a file,
// a path value from graphvein.yaml marked as set by you, and the usage
// and not-indexed refusals.
func TestOpenAPICommand(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GRAPHVEIN_SQLITE_PATH", filepath.Join(root, ".graphvein", "index.db"))
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	files := map[string]string{
		"package.json": `{"name":"app","dependencies":{"express":"^4.19.0"}}`,
		"app.js":       "const express = require('express');\nconst app = express();\nconst orphan = express.Router();\napp.get('/items/:id', (req, res) => res.end());\norphan.get('/lost', (req, res) => res.end());\n",
		"graphvein.yaml": "version: 1\nexplorer:\n  path_values:\n    - key: \"route:app.js#GET%20%2Flost\"\n      value: /api/lost\n" +
			"tryit:\n  environments:\n    - name: local\n      label: local\n      base_url: http://127.0.0.1:3000/\n      auth: {header: Authorization, env: API_TOKEN}\n",
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"openapi", root}, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "not indexed") {
		t.Fatalf("before indexing: %d %s", code, errOut.String())
	}
	if code := run([]string{"index", root}, &out, &errOut); code != 0 {
		t.Fatal(errOut.String())
	}
	out.Reset()
	if code := run([]string{"openapi", root}, &out, &errOut); code != 0 {
		t.Fatal(errOut.String())
	}
	var doc struct {
		OpenAPI string                    `json:"openapi"`
		Servers []map[string]any          `json:"servers"`
		Paths   map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil || doc.OpenAPI != "3.1.1" {
		t.Fatalf("%v %s", err, out.String())
	}
	if len(doc.Servers) != 1 || doc.Servers[0]["url"] != "http://127.0.0.1:3000" || strings.Contains(out.String(), "API_TOKEN") {
		t.Errorf("servers %v (an auth setting must never be in the document)", doc.Servers)
	}
	if doc.Paths["/items/{id}"]["get"] == nil {
		t.Errorf("paths %v", doc.Paths)
	}
	lost, _ := doc.Paths["/api/lost"]["get"].(map[string]any)
	if lost == nil || lost["x-graphvein"].(map[string]any)["resolution"] != "set_by_you" {
		t.Errorf("value set by you: %v", doc.Paths["/api/lost"])
	}
	file := filepath.Join(t.TempDir(), "api.yaml")
	if code := run([]string{"openapi", root, "--format", "yaml", "--out", file}, &out, &errOut); code != 0 {
		t.Fatal(errOut.String())
	}
	data, err := os.ReadFile(file)
	var fromYAML map[string]any
	if err != nil || yaml.Unmarshal(data, &fromYAML) != nil || fromYAML["openapi"] != "3.1.1" {
		t.Fatalf("yaml file: %v %s", err, data)
	}
	for _, args := range [][]string{{"openapi", root, "--format", "xml"}, {"openapi", root, root}, {"openapi", "--project", root, root}} {
		if code := run(args, &out, &errOut); code != 2 {
			t.Errorf("%v: exit %d", args, code)
		}
	}
}

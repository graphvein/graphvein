package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenAPIBasePaths(t *testing.T) {
	for _, mapping := range []string{
		"[]", "null", "{a: /x, a: /y}", "{'../a': /x}", "{'a/': /x}",
		"{'C:/a': /x}", "{a: relative}", "{a: null}", "{a: 1}",
		"{a: &x /x, b: *x}", "{a: \"/a\\0b\"}",
	} {
		t.Run(mapping, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte("version: 1\nopenapi:\n  base_paths: "+mapping+"\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(root); err == nil {
				t.Fatal("accepted", mapping)
			}
		})
	}
	root := t.TempDir()
	file := filepath.Join(root, "graphvein.yaml")
	if err := os.WriteFile(file, []byte("version: 1\nopenapi:\n  include: ['contracts/**']\n  exclude: ['contracts/old.json']\n  base_paths: {'.': '', contracts: /api, 'contracts/openapi.json': /v2}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(root)
	if err != nil || len(c.OpenAPIBasePaths) != 3 || c.OpenAPIBasePaths["contracts/openapi.json"] != "/v2" {
		t.Fatal(c, err)
	}
	if included, excluded := c.OpenAPI.Match("contracts/old.json"); !included || !excluded {
		t.Fatal("discovery rules lost")
	}
	if err := os.WriteFile(file, []byte("version: 1\nopenapi: {base_paths: {}, base_paths: {}}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Fatal("accepted duplicate base_paths")
	}
}

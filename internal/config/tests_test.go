package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/testscope"
)

func TestTestsStrictDecoding(t *testing.T) {
	for _, content := range []string{
		"tests: []", "tests: null", "tests: {unknown: []}", "tests: {include: [], include: []}",
		"tests: {include: '*.ts'}", "tests: {exclude: [false]}", "tests: {include: ['']}",
		"tests: {include: ['!x']}", "tests: {include: ['a\\b']}", "tests: {include: ['[']}",
		"tests: {include: [\"a\\0b\"]}", "tests: {include: [], exclude: [], exclude: []}",
		"tests: {include: &p [] , exclude: *p}", "tests: {}\ntests: {}",
	} {
		t.Run(content, func(t *testing.T) {
			root := t.TempDir()
			file := filepath.Join(root, "graphvein.yaml")
			if err := os.WriteFile(file, []byte("version: 1\n"+content+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(root)
			if err == nil || !strings.Contains(err.Error(), file) || !strings.Contains(err.Error(), "line") {
				t.Fatalf("error=%v", err)
			}
		})
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte("version: 1\ntests:\n  include: ['checks/**']\n  exclude: ['tests/**']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if testscope.Classify("checks/a.ts", c.Tests) != testscope.Test || testscope.Classify("tests/a.ts", c.Tests) != "" {
		t.Fatal("configuration not applied")
	}
}

func TestOpenAPIStrictDecoding(t *testing.T) {
	for _, content := range []string{
		"openapi: []", "openapi: null", "openapi: {unknown: []}",
		"openapi: {include: [], include: []}", "openapi: {include: '*.json'}",
		"openapi: {exclude: [false]}", "openapi: {include: ['']}",
		"openapi: {include: ['!x']}", "openapi: {include: ['a\\b']}",
		"openapi: {include: ['[']}", "openapi: {include: [\"a\\0b\"]}",
		"openapi: {include: &p [], exclude: *p}", "openapi: {}\nopenapi: {}",
	} {
		t.Run(content, func(t *testing.T) {
			root := t.TempDir()
			file := filepath.Join(root, "graphvein.yaml")
			if err := os.WriteFile(file, []byte("version: 1\n"+content+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(root)
			if err == nil || !strings.Contains(err.Error(), file) || !strings.Contains(err.Error(), "line") {
				t.Fatalf("error=%v", err)
			}
		})
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte("version: 1\nopenapi:\n  include: ['checks/**']\n  exclude: ['checks/no.json']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	i, e := c.OpenAPI.Match("checks/no.json")
	if !i || !e {
		t.Fatal("patterns not applied")
	}
	i, e = c.OpenAPI.Match("testdata/not-included.json")
	if i || e {
		t.Fatal("test built-ins affected discovery")
	}
}

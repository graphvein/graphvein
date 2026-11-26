package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClientsConfig(t *testing.T) {
	for _, body := range []string{
		"clients: []", "clients: {unknown: x}", "clients: {base_paths: []}",
		"clients: {base_paths: {'../src': /api}}", "clients: {base_paths: {'src/': /api}}",
		"clients: {base_paths: {'C:/src': /api}}", "clients: {base_paths: {src: relative}}",
		"clients: {base_paths: {src: null}}", "clients: {base_paths: {src: 1}}",
		"clients: {base_paths: {src: /a, src: /b}}", "clients: {base_paths: {}, base_paths: {}}",
		"clients: {base_paths: {src: &base /api, other: *base}}",
		"clients: {link_generated_output: yes}", "clients: {link_generated_output: 'true'}",
		"clients: {link_generated_output: [true]}", "clients: {link_generated_output: true, link_generated_output: false}",
	} {
		t.Run(body, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte("version: 1\n"+body+"\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(root); err == nil {
				t.Fatal("accepted", body)
			}
		})
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte("version: 1\nclients:\n  base_paths:\n    '.': ''\n    src/雪: /api\n"), 0644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Clients.BasePaths) != 2 || c.Clients.BasePaths["src/雪"] != "/api" || c.Clients.LinkGeneratedOutput {
		t.Fatal(c)
	}
	// The opt-in of ADR 0085 links committed bundler output.
	for body, want := range map[string]bool{"clients: {link_generated_output: true}": true, "clients: {link_generated_output: false}": false} {
		c, err := Parse("graphvein.yaml", []byte("version: 1\n"+body+"\n"))
		if err != nil || c.Clients.LinkGeneratedOutput != want || c.Clients.BasePaths == nil {
			t.Errorf("%s: %+v %v", body, c.Clients, err)
		}
	}
}

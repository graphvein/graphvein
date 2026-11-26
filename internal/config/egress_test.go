package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEgressConfig(t *testing.T) {
	load := func(t *testing.T, body string) (Config, error, string) {
		t.Helper()
		root := t.TempDir()
		path := filepath.Join(root, "graphvein.yaml")
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
		c, err := Load(root)
		return c, err, path
	}
	for body, want := range map[string]string{
		"version: 1\n":                          "offline",
		"version: 1\negress: {}\n":              "offline",
		"version: 1\negress: {mode: offline}\n": "offline",
		"version: 1\negress:\n  mode: llm\n":    "llm",
	} {
		c, err, _ := load(t, body)
		if err != nil || c.Egress.Mode != want {
			t.Fatalf("%q: mode %q, err %v", body, c.Egress.Mode, err)
		}
	}
	if c, err := Load(t.TempDir()); err != nil || c.Egress.Mode != "offline" {
		t.Fatal("missing file", c.Egress, err)
	}
	for name, body := range map[string]string{
		"unknown":       "version: 1\negress: {mode: offline, proxy: x}\n",
		"duplicate":     "version: 1\negress: {mode: offline, mode: llm}\n",
		"duplicate-key": "version: 1\negress: {}\negress: {}\n",
		"alias":         "version: 1\nrepository: &m offline\negress: {mode: *m}\n",
		"alias-section": "version: 1\nide: &e {}\negress: *e\n",
		"sequence":      "version: 1\negress: [offline]\n",
		"scalar":        "version: 1\negress: offline\n",
		"null":          "version: 1\negress:\n",
		"number":        "version: 1\negress: {mode: 1}\n",
		"bool":          "version: 1\negress: {mode: true}\n",
		"null-mode":     "version: 1\negress: {mode: null}\n",
		"bad-value":     "version: 1\negress: {mode: online}\n",
		"case":          "version: 1\negress: {mode: Offline}\n",
		"empty":         "version: 1\negress: {mode: ''}\n",
		"tagged":        "version: 1\negress: {mode: !!binary b2ZmbGluZQ==}\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err, path := load(t, body)
			if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "line") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

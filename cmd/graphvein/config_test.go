package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigUsageErrorsAcrossCommands(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "graphvein.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nunknown: x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"index", "status", "query", "callers", "impact"} {
		args := []string{command}
		if command != "index" && command != "status" {
			args = append(args, "target")
		}
		args = append(args, root, "--repository", "override")
		var out, errors bytes.Buffer
		if code := run(args, &out, &errors); code != 2 || !strings.Contains(errors.String(), path) || !strings.Contains(errors.String(), "line 2") {
			t.Fatal(command, code, errors.String())
		}
	}
}

func TestExternalCLIUsageAndPreflight(t *testing.T) {
	for _, option := range []string{"--workers=0", "--workers=1", "--timeout=5s", "--max-file-size=1", "--shape-depth=6"} {
		var out, errors bytes.Buffer
		if code := run([]string{"index", "--indexer", os.Args[0], option}, &out, &errors); code != 2 {
			t.Fatal(option, code, errors.String())
		}
	}
	var out, errors bytes.Buffer
	if code := run([]string{"index", "--indexer", os.Args[0], "--indexer-timeout=-1s"}, &out, &errors); code != 2 {
		t.Fatal(code, errors.String())
	}
	for _, option := range []string{"--shape-depth=0", "--shape-depth=11", "--shape-depth=-1"} {
		errors.Reset()
		if code := run([]string{"index", t.TempDir(), option}, &out, &errors); code != 2 || !strings.Contains(errors.String(), "--shape-depth must be from 1 to 10") {
			t.Fatal(option, code, errors.String())
		}
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte("version: 2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	errors.Reset()
	if code := run([]string{"index", root, "--indexer", filepath.Join(root, "missing.exe")}, &out, &errors); code != 1 || !strings.Contains(errors.String(), "checking indexer") {
		t.Fatal(code, errors.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".graphvein")); !os.IsNotExist(err) {
		t.Fatal("preflight created storage", err)
	}
}

//go:build graphveinprofile

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Snapshot equivalence must ignore only the timestamp, retaining body bytes.
func TestNormalizeSnapshot(t *testing.T) {
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "a"), filepath.Join(dir, "b"), filepath.Join(dir, "c")}
	streams := []string{
		"{\"type\":\"header\",\"generated_at\":\"first\",\"repository\":\"r\"}\n{\"type\":\"file\",\"path\":\"a\"}\n",
		"{\"type\":\"header\",\"generated_at\":\"second\",\"repository\":\"r\"}\n{\"type\":\"file\",\"path\":\"a\"}\n",
		"{\"type\":\"header\",\"generated_at\":\"second\",\"repository\":\"r\"}\n{\"type\":\"file\",\"path\":\"b\"}\n",
	}
	for i, stream := range streams {
		if err := os.WriteFile(paths[i], []byte(stream), 0644); err != nil {
			t.Fatal(err)
		}
	}
	a, err := normalize(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	b, err := normalize(paths[1])
	if err != nil {
		t.Fatal(err)
	}
	c, err := normalize(paths[2])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("timestamp affected equivalence")
	}
	if bytes.Equal(a, c) {
		t.Fatal("body difference was hidden")
	}
	// Property order also remains significant in this byte-equivalence check.
	if err := os.WriteFile(paths[2], []byte("{\"repository\":\"r\",\"generated_at\":\"second\",\"type\":\"header\"}\n{\"type\":\"file\",\"path\":\"a\"}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	c, err = normalize(paths[2])
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, c) {
		t.Fatal("header byte ordering difference was hidden")
	}
}

func TestCopyLeavesSourceUntouched(t *testing.T) {
	src, dest := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(src, ".graphvein"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, ".graphvein", "index.db"), []byte("scratch"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "source.py"), []byte("pass\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(src, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".graphvein")); !os.IsNotExist(err) {
		t.Fatal("copied source scratch directory")
	}
	if err := os.WriteFile(filepath.Join(dest, "source.py"), []byte("pass\n# edit\n"), 0644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(src, "source.py"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "pass\n" {
		t.Fatal("source changed")
	}
}

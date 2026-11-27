package scan

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func write(t *testing.T, root, p, source string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(p))
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
}
func TestIgnoresBinaryAndHash(t *testing.T) {
	root := t.TempDir()
	for p, s := range map[string]string{".gitignore": "*.log\nfolder/\n!keep.log\n**/generated/*.ts\n", ".graphveinignore": "extra.ts\n!keep.log\n", "nested/.gitignore": "*.ts\n!keep.ts\n", "nested/keep.ts": "const a = 1;\r\n", "nested/drop.ts": "ignored", "folder/skip.ts": "ignored", "a/generated/drop.ts": "ignored", ".codex/hidden.ts": "ignored", "node_modules/package/a.ts": "ignored", "extra.ts": "ignored", "keep.log": "kept", "drop.log": "ignored", "binary.bin": "a\x00b", "script": "#!/usr/bin/env node\nconsole.log(1)", "readme.txt": "plain"} {
		write(t, root, p, s)
	}
	result, err := Walk(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{}
	for _, f := range result.Files {
		paths = append(paths, f.Record.Path)
		if f.Record.Path == "nested/keep.ts" && string(f.Source) != "const a = 1;\n" {
			t.Fatal("source not normalized")
		}
		if f.Record.Path == "script" && f.Record.Language != "javascript" {
			t.Fatal("shebang missed")
		}
	}
	want := []string{".gitignore", ".graphveinignore", "keep.log", "nested/.gitignore", "nested/keep.ts", "readme.txt", "script"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatal(paths)
	}
}
func TestSymlinkBoundary(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	write(t, outside, "secret.ts", "secret")
	if err := os.Symlink(outside, filepath.Join(root, "outside")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	result, err := Walk(context.Background(), root)
	if err != nil || len(result.Files) != 0 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "symlink" {
		t.Fatal(result, err)
	}
	// A symlink to a directory is listed as a skipped directory: Python's
	// import system follows it.
	if !reflect.DeepEqual(result.Skipped, []string{"outside/"}) {
		t.Fatal(result.Skipped)
	}
}

// TestSkippedAndDirs checks that every entry the walk does not read is in
// Skipped and every directory it enters in Dirs, even one
// holding no file (B5).
func TestSkippedAndDirs(t *testing.T) {
	root := t.TempDir()
	write(t, root, "pkg/mod.py", "x = 1\n")
	write(t, root, "data/raw/sample.bin", "a\x00b")
	write(t, root, "node_modules/x.js", "x")
	if err := os.MkdirAll(filepath.Join(root, "empty", "deeper"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("pkg/mod.py", filepath.Join(root, "link.py")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	result, err := Walk(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"data/raw/sample.bin", "link.py", "node_modules/"}; !reflect.DeepEqual(result.Skipped, want) {
		t.Errorf("skipped %v, want %v", result.Skipped, want)
	}
	if want := []string{"data", "data/raw", "empty", "empty/deeper", "pkg"}; !reflect.DeepEqual(result.Dirs, want) {
		t.Errorf("dirs %v, want %v", result.Dirs, want)
	}
}
func TestGitBlobMatchesGit(t *testing.T) {
	root := t.TempDir()
	if output, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Skip(string(output))
	}
	write(t, root, "a.ts", "const a = 1;\r\n")
	result, err := Walk(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	oid, err := exec.Command("git", "-c", "safe.directory="+filepath.ToSlash(root), "-C", root, "hash-object", "--no-filters", "a.ts").Output()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 1 || result.Files[0].Record.GitBlobOID+"\n" != string(oid) {
		t.Fatal(result.Files, string(oid))
	}
}

func TestRecognizedLanguagesAndVendor(t *testing.T) {
	for _, tc := range []struct{ path, source, want string }{
		{"a.go", "", "go"}, {"a.rs", "", "rust"}, {"a.py", "", "python"}, {"a.pyi", "", "python"},
		{"a.java", "", "java"}, {"a.cs", "", "c_sharp"}, {"a.dart", "", "dart"}, {"a.html", "", "html"},
		{"a.css", "", "css"}, {"a.json", "", "json"}, {"a.yml", "", "yaml"}, {"a.yaml", "", "yaml"},
		{"a.toml", "", "toml"}, {"a.md", "", "markdown"}, {"a.sql", "", "sql"}, {"a.sh", "", "shell"},
		{"script", "#!/usr/bin/env python3\n", "python"}, {"script", "#!/usr/bin/python\n", "python"},
		{"script", "#!/usr/bin/env -S bash -eu\n", "shell"}, {"script", "#!/bin/sh\n", "shell"},
		{"script", "#!/usr/bin/env fish\n", "unknown"}, {"readme.txt", "text", "unknown"},
	} {
		if got := Language(tc.path, []byte(tc.source)); got != tc.want {
			t.Errorf("%s %q = %s, want %s", tc.path, tc.source, got, tc.want)
		}
	}
	root := t.TempDir()
	write(t, root, "vendor/hidden.go", "package hidden")
	write(t, root, "main.go", "package main")
	scanned, err := Walk(context.Background(), root)
	if err != nil || len(scanned.Files) != 1 || scanned.Files[0].Record.Path != "main.go" {
		t.Fatal(scanned, err)
	}
}

func TestAgentFolderIgnores(t *testing.T) {
	root := t.TempDir()
	for _, base := range []string{"", "nested"} {
		destination := filepath.Join(root, base)
		if err := os.CopyFS(destination, os.DirFS("../../testdata/fixtures/agent-ignores")); err != nil {
			t.Fatal(err)
		}
		for _, names := range [][2]string{{"_agents", ".agents"}, {"_cursor", ".cursor"}} {
			if err := os.Rename(filepath.Join(destination, names[0]), filepath.Join(destination, names[1])); err != nil {
				t.Fatal(err)
			}
		}
	}
	result, err := Walk(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, file := range result.Files {
		paths = append(paths, file.Record.Path)
	}
	want := []string{"README.md", "nested/README.md", "nested/visible.ts", "visible.ts"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("scanned %v, want %v", paths, want)
	}
}

// TestSkippedEntries checks that the walk reports what it saw but did not
// read (Result.Skipped): ignored and default-skipped directories with a
// trailing slash, ignored files, binary and non-UTF-8 files.
func TestSkippedEntries(t *testing.T) {
	root := t.TempDir()
	for p, s := range map[string]string{".gitignore": "*.log\n", "a.py": "x = 1\n", "drop.log": "x", "build/pkg/__init__.py": "", "mod.cpython-314-x86_64-linux-gnu.so": "\x7fELF\x00", "latin1.py": "x = '\xe9'\n", "pkg/inner.pyc": "\x00\x01"} {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	result, err := Walk(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"build/", "drop.log", "latin1.py", "mod.cpython-314-x86_64-linux-gnu.so", "pkg/inner.pyc"}
	if !reflect.DeepEqual(result.Skipped, want) {
		t.Fatalf("skipped %v, want %v", result.Skipped, want)
	}
	// Binary and non-UTF-8 files the rules keep get a record; ignored and
	// default names do not (couldn't-read rows CR-34, CR-35).
	records := map[string]string{}
	for _, d := range result.Diagnostics {
		records[d.File] = d.Code + ":" + d.Severity
	}
	wantRecords := map[string]string{"latin1.py": "file_not_utf8:warning", "mod.cpython-314-x86_64-linux-gnu.so": "file_binary:info", "pkg/inner.pyc": "file_binary:info"}
	if !reflect.DeepEqual(records, wantRecords) {
		t.Errorf("records %v, want %v", records, wantRecords)
	}
}

// TestDeliberateSkip pins the positive list of skip codes: the entries the
// walk chooses not to read are skips, every walk failure is not.
func TestDeliberateSkip(t *testing.T) {
	for _, code := range []string{"symlink", "file_not_regular", "file_binary", "file_not_utf8"} {
		if !DeliberateSkip(code) {
			t.Errorf("%s is not a deliberate skip", code)
		}
	}
	for _, code := range []string{"file_read", "scan_read", "ignore_read", "invalid_path", ""} {
		if DeliberateSkip(code) {
			t.Errorf("%s is a deliberate skip", code)
		}
	}
}

// TestRustSourceWithNUL checks the NUL exception of the binary rule: a Rust
// file holding a few raw NUL bytes (legal inside literals and comments) is
// read as source, while a Rust file made mostly of NUL bytes, a non-UTF-8
// one, and files of other languages with a NUL stay skipped as binary.
func TestRustSourceWithNUL(t *testing.T) {
	root := t.TempDir()
	dense := "fn f() {}\n" + strings.Repeat("\x00", 64)
	files := map[string]string{
		"lit.rs":    "#[test]\nfn t() { assert_eq!(b\"BM\x00\x00\".len(), 4); }\n",
		"dense.rs":  dense,
		"latin.rs":  "fn f() { let _ = b\"\x00\"; } // \xe9\n",
		"script.py": "x = '\x00'\n",
		"main.go":   "package main\n// \x00\n",
	}
	for p, s := range files {
		if err := os.WriteFile(filepath.Join(root, p), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	result, err := Walk(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	var read []string
	for _, f := range result.Files {
		read = append(read, f.Record.Path)
		if f.Record.Path == "lit.rs" && (f.Record.Language != "rust" || string(f.Source) != files["lit.rs"]) {
			t.Errorf("lit.rs read as %q with source %q", f.Record.Language, f.Source)
		}
	}
	if !reflect.DeepEqual(read, []string{"lit.rs"}) {
		t.Errorf("read %v, want [lit.rs]", read)
	}
	records := map[string]string{}
	for _, d := range result.Diagnostics {
		records[d.File] = d.Code
	}
	want := map[string]string{"dense.rs": "file_binary", "latin.rs": "file_binary", "script.py": "file_binary", "main.go": "file_binary"}
	if !reflect.DeepEqual(records, want) {
		t.Errorf("records %v, want %v", records, want)
	}
}

package index

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// TestDartExtractorPanicIsFileLocal: an extractor panic on one Dart file is
// an extract_failed error for that file only (row BE-3); the other file
// keeps its nodes, its relative import of the failed file still names that
// scanned file (DA-I2), and its dart: import stays external (row BE-8 does
// not apply to Dart).
func TestDartExtractorPanicIsFileLocal(t *testing.T) {
	root := t.TempDir()
	for name, source := range map[string]string{"a.dart": "class A { void a() {} }\n", "b.dart": "import 'a.dart';\nimport 'dart:io';\nclass B { void b() {} }\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	extract := func(file string, source []byte, root *parse.Node, language string) (typescript.Facts, error) {
		if file == "a.dart" {
			panic("injected")
		}
		return extractFile(file, source, root, language)
	}
	var out bytes.Buffer
	if _, err := emit(context.Background(), root, s, &out, Options{Repository: "fixture", Parser: parse.Options{CacheDir: filepath.Join(t.TempDir(), "wazero")}}, extract); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{`"file":"a.dart","severity":"error","code":"extract_failed"`, `"identity":"method:b.dart#B.b"`, `"kind":"imports","source":"file:b.dart#b.dart","target":"file:a.dart#a.dart"`, `"identity":"external:.#dart%3Aio"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s in\n%s", want, text)
		}
	}
	if strings.Contains(text, `"identity":"class:a.dart#A"`) || strings.Contains(text, "unresolved_import") {
		t.Fatal(text)
	}
}

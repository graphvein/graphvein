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

// TestCSharpExtractorPanicIsFileLocal: an extractor panic on one C# file is
// an extract_failed error for that file only (row BE-3); the other file
// keeps its nodes, and its using that would be external is a BE-8 warning,
// since the failed file could declare that namespace.
func TestCSharpExtractorPanicIsFileLocal(t *testing.T) {
	root := t.TempDir()
	for name, source := range map[string]string{"A.cs": "class A { void a() {} }\n", "B.cs": "using System.Text;\nclass B { void b() {} }\n"} {
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
		if file == "A.cs" {
			panic("injected")
		}
		return extractFile(file, source, root, language)
	}
	var out bytes.Buffer
	if _, err := emit(context.Background(), root, s, &out, Options{Repository: "fixture", Parser: parse.Options{CacheDir: filepath.Join(t.TempDir(), "wazero")}}, extract); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, `"file":"A.cs","severity":"error","code":"extract_failed"`) || !strings.Contains(text, `"identity":"method:B.cs#B.b"`) || strings.Contains(text, `"identity":"class:A.cs#A"`) || !strings.Contains(text, `"file":"B.cs","severity":"warning","code":"unresolved_import"`) || strings.Contains(text, `dotnet`) {
		t.Fatal(text)
	}
}

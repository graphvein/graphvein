package typescript_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// labelMaxFileSize bounds the files the label test parses: the fixture's
// files are far smaller, and big/big.ts, written by the test, is larger.
const labelMaxFileSize = 64 << 10

// TestTypeScriptFileLabels checks the stored TypeScript, TSX and JavaScript
// file labels of row PL-L3 (T020) on the structure fixture, one
// file too large to parse and one TSX file tree-sitter accepts, cold and
// from the fact cache: every such file is precise, declaration files
// included, except a file whose structure
// claims nothing, which is stored best_effort in its file record and its
// file node. Which files those are is decided here, apart from the
// indexer's label code: tree-sitter reports an error in the raw bytes on
// disk (TS-F2, parsed again here) or the file is larger than the parse
// limit (TS-F3). Files of other languages stay best_effort.
func TestTypeScriptFileLabels(t *testing.T) {
	ctx := context.Background()
	root := copyFixture(t)
	big := "// " + strings.Repeat("x", labelMaxFileSize) + "\nexport function big() {}\n"
	if err := os.MkdirAll(filepath.Join(root, "big"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "big", "big.ts"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	// Both TSX files of the fixture are rejected cases; one tree-sitter
	// accepts.
	if err := os.WriteFile(filepath.Join(root, "big", "view.tsx"), []byte("export const View = () => <div>ok</div>;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	parser := parse.Options{Workers: 2, CacheDir: filepath.Join(t.TempDir(), "cache")}
	opt := index.Options{Repository: "fixture", GeneratedAt: "2026-11-17T00:00:00Z", MaxFileSize: labelMaxFileSize, Parser: parser}
	for run := 0; run < 2; run++ {
		var stream bytes.Buffer
		if _, err := index.Emit(ctx, root, s, &stream, opt); err != nil {
			t.Fatal(err)
		}
		if _, err := ingest.StreamWithWrites(ctx, &stream, s); err != nil {
			t.Fatal(err)
		}
		files, err := s.Files(ctx, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := s.Dump(ctx, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		nodeLabel := map[string]any{}
		for _, n := range snapshot.Nodes {
			if n.Kind == "file" {
				nodeLabel[n.File] = n.Attributes["precision"]
			}
		}
		reached := map[string]int{}
		for _, f := range files {
			want := "best_effort"
			if f.Language == "typescript" || f.Language == "tsx" || f.Language == "javascript" {
				raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f.Path)))
				if err != nil {
					t.Fatal(err)
				}
				switch {
				case len(raw) > labelMaxFileSize:
					reached["too_large"]++
				case syntaxError(t, f.Language, raw, parser):
					reached["rejected"]++
				default:
					want = "precise"
					reached[f.Language]++
					if strings.HasSuffix(f.Path, ".d.ts") {
						reached["declaration_file"]++
					}
				}
			}
			if f.Precision != want {
				t.Errorf("run %d: %s (%s): stored precision %q, want %q", run, f.Path, f.Language, f.Precision, want)
			}
			if got := nodeLabel[f.Path]; got != want {
				t.Errorf("run %d: %s: file node precision %v, want %q", run, f.Path, got, want)
			}
		}
		for _, c := range []string{"too_large", "rejected", "typescript", "tsx", "javascript", "declaration_file"} {
			if reached[c] == 0 {
				t.Errorf("run %d: no file reaches case %s", run, c)
			}
		}
	}
}

// syntaxError parses source with tree-sitter on its own and reports
// whether the tree holds an error (row TS-F2).
func syntaxError(t *testing.T, language string, source []byte, options parse.Options) bool {
	t.Helper()
	results, err := parse.Batch(context.Background(), []parse.Job{{Language: language, Source: source}}, options)
	if err != nil || len(results) != 1 || results[0].Err != nil {
		t.Fatalf("parsing a %s file: %v %v", language, err, results)
	}
	return results[0].SyntaxError
}

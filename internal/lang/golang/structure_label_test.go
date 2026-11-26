package golang_test

import (
	"bytes"
	"context"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/lang/golang"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// labelMaxFileSize bounds the files the label test parses: the fixture's
// files are far smaller, and big/big.go, written by the test, is larger.
const labelMaxFileSize = 64 << 10

// TestGoFileLabels checks the stored Go file labels of row PL-L1 (T020)
// on the structure fixture, its written cases and one file too large
// to parse, cold and from the fact cache: every Go file is precise, test
// files included, except a file whose structure claims nothing, which is
// stored best_effort in its file record and its file node. Which files
// those are is decided here from the raw bytes on disk, apart from the
// indexer: go/parser rejects the file (GS-F2), it holds a CR not followed
// by LF (GS-F3), or it is larger than the parse limit. Files of other
// languages stay best_effort.
func TestGoFileLabels(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(structureFixture)); err != nil {
		t.Fatal(err)
	}
	golang.WriteStructureExtraFiles(t, root)
	big := "package big\n\n// " + strings.Repeat("x", labelMaxFileSize) + "\nfunc Big() {}\n"
	if err := os.MkdirAll(filepath.Join(root, "big"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "big", "big.go"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	opt := index.Options{Repository: "fixture", GeneratedAt: "2026-11-16T00:00:00Z", MaxFileSize: labelMaxFileSize, Parser: parse.Options{Workers: 2, CacheDir: filepath.Join(t.TempDir(), "cache")}}
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
			if f.Language == "go" {
				raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f.Path)))
				if err != nil {
					t.Fatal(err)
				}
				_, perr := parser.ParseFile(token.NewFileSet(), f.Path, raw, parser.SkipObjectResolution)
				loneCR := bytes.Count(raw, []byte("\r")) != bytes.Count(raw, []byte("\r\n"))
				switch {
				case loneCR:
					reached["lone_cr"]++
				case perr != nil:
					reached["rejected"]++
				case len(raw) > labelMaxFileSize:
					reached["too_large"]++
				default:
					want = "precise"
					if strings.HasSuffix(path.Base(f.Path), "_test.go") {
						reached["test_file"]++
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
		for _, c := range []string{"lone_cr", "rejected", "too_large", "test_file"} {
			if reached[c] == 0 {
				t.Errorf("run %d: no Go file reaches case %s", run, c)
			}
		}
	}
}

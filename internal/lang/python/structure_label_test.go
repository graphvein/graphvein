package python_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// labelMaxFileSize bounds the files the label test parses: the fixture's
// files are far smaller, and big/big.py, written by the test, is larger.
const labelMaxFileSize = 64 << 10

// astCheck prints, for each path argument, "ok" or "rejected" as CPython's
// own ast.parse over the raw bytes decides (row PS-F2's reference).
const astCheck = `import ast, sys
for p in sys.argv[1:]:
    try:
        ast.parse(open(p, "rb").read(), p)
        print("ok")
    except (SyntaxError, ValueError, UnicodeDecodeError):
        print("rejected")
`

// TestPythonFileLabels checks the stored Python file labels of row PL-L2
// (T020) on the structure fixture, its written cases and one file
// too large to parse, cold and from the fact cache: every Python file is
// precise, test files included, except a file whose structure claims
// nothing, which is stored best_effort in its file record and its file
// node. Which files those are is decided apart from the indexer: the
// reference CPython's ast.parse rejects the raw bytes (PS-F2), or the file
// is larger than the parse limit. Files of other languages stay
// best_effort. Skips without a CPython 3.14 (GRAPHVEIN_PYTHON_ORACLE).
func TestPythonFileLabels(t *testing.T) {
	interpreter := oracleInterpreter(t)
	ctx := context.Background()
	root := copyStructureFixture(t)
	big := "# " + strings.Repeat("x", labelMaxFileSize) + "\ndef big():\n    pass\n"
	if err := os.MkdirAll(filepath.Join(root, "big"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "big", "big.py"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	opt := index.Options{Repository: "fixture", GeneratedAt: "2026-11-17T00:00:00Z", MaxFileSize: labelMaxFileSize, Parser: parse.Options{Workers: 2, CacheDir: filepath.Join(t.TempDir(), "cache")}}
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
		var pyPaths []string
		for _, f := range files {
			if f.Language == "python" {
				pyPaths = append(pyPaths, filepath.Join(root, filepath.FromSlash(f.Path)))
			}
		}
		out, err := exec.Command(interpreter, append([]string{"-I", "-c", astCheck}, pyPaths...)...).Output()
		if err != nil {
			t.Fatalf("running %s: %v", interpreter, err)
		}
		verdicts := strings.Fields(string(out))
		if len(verdicts) != len(pyPaths) {
			t.Fatalf("%d ast verdicts for %d Python files", len(verdicts), len(pyPaths))
		}
		reached := map[string]int{}
		next := 0
		for _, f := range files {
			want := "best_effort"
			if f.Language == "python" {
				verdict := verdicts[next]
				next++
				info, err := os.Stat(filepath.Join(root, filepath.FromSlash(f.Path)))
				if err != nil {
					t.Fatal(err)
				}
				switch {
				case verdict == "rejected":
					reached["rejected"]++
				case info.Size() > labelMaxFileSize:
					reached["too_large"]++
				default:
					want = "precise"
					if strings.HasPrefix(path.Base(f.Path), "test_") {
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
		for _, c := range []string{"rejected", "too_large"} {
			if reached[c] == 0 {
				t.Errorf("run %d: no Python file reaches case %s", run, c)
			}
		}
	}
}

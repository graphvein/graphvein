package rust_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/lang/rust"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// labelMaxFileSize bounds the files the label test parses: the fixture's
// files are far smaller, and app/src/big.rs, written by the test, is larger.
const labelMaxFileSize = 64 << 10

// refusedRustFiles are the fixture's Rust files whose structure claims
// nothing (stored best_effort, reason structure_refused), by case, from the
// fixture README: tree-sitter reports a syntax error (RS-F2), a lone CR
// (RS-F3), and the file the test writes above the parse limit.
var refusedRustFiles = map[string]string{
	"app/src/broken.rs": "syntax_error",
	"app/src/lonecr.rs": "lone_cr",
	"app/src/big.rs":    "too_large",
}

// unrootedRustFiles are the fixture's Rust files no crate root of a read
// Cargo.toml reaches (stored best_effort with a structure_unrooted
// diagnostic), from the fixture README and the oracle's `cargo metadata`
// targets and rustc module trees: decoys at default module places, both
// candidates of an E0761 module, an `include!` target, files under an
// unreadable manifest, and targets the manifest's discovery keys turn off.
var unrootedRustFiles = []string{
	"app/src/alts/commented.rs", "app/src/alts/documented.rs", "app/src/alts/foo/bar/c.rs",
	"app/src/both.rs", "app/src/both/mod.rs", "app/src/included.rs", "app/src/unreached.rs",
	"bad/src/lib.rs", "bad/src/sub.rs", "stray/orphan.rs",
	"targets/examples/other.rs", "targets/examples/skipped.rs", "targets/src/bin/auto.rs",
	"targets/src/bin/helper.rs", "targets/src/helper.rs", "targets/src/main.rs",
}

// TestRustFileLabels checks the stored Rust file labels of row PL-L4
// (T020) on the structure fixture, its written cases and one file too
// large to parse, cold and from the fact cache: every Rust file a crate root
// reaches is precise (tests, examples, benches and build scripts
// included), a file whose structure claims nothing is stored best_effort,
// and a file no crate root reaches is stored best_effort with one
// structure_unrooted diagnostic, in its file record and its file node.
// Files of other languages stay best_effort.
func TestRustFileLabels(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(structureFixture)); err != nil {
		t.Fatal(err)
	}
	rust.WriteStructureExtraFiles(t, root)
	big := "// " + strings.Repeat("x", labelMaxFileSize) + "\npub fn big() {}\n"
	if err := os.WriteFile(filepath.Join(root, "app", "src", "big.rs"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	unrooted := map[string]bool{}
	for _, f := range unrootedRustFiles {
		unrooted[f] = true
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
		diagnosed := map[string]int{}
		for _, d := range snapshot.Diagnostics {
			if d.Code == "structure_unrooted" {
				diagnosed[d.File]++
				if d.Severity != "info" {
					t.Errorf("run %d: %s: structure_unrooted severity %s, want info", run, d.File, d.Severity)
				}
			}
		}
		reached := map[string]int{}
		for _, f := range files {
			want := "best_effort"
			if f.Language == "rust" {
				switch c, refused := refusedRustFiles[f.Path]; {
				case refused:
					reached[c]++
				case unrooted[f.Path]:
					reached["unrooted"]++
				default:
					want = "precise"
					switch {
					case strings.Contains(f.Path, "/tests/") || strings.Contains(f.Path, "/checks/"):
						reached["test_target"]++
					case strings.HasSuffix(f.Path, "build.rs") || strings.Contains(f.Path, "/tools/"):
						reached["build_script"]++
					}
				}
			}
			if f.Precision != want {
				t.Errorf("run %d: %s (%s): stored precision %q, want %q", run, f.Path, f.Language, f.Precision, want)
			}
			if got := nodeLabel[f.Path]; got != want {
				t.Errorf("run %d: %s: file node precision %v, want %q", run, f.Path, got, want)
			}
			wantDiagnostics := 0
			if f.Language == "rust" && unrooted[f.Path] {
				wantDiagnostics = 1
			}
			if diagnosed[f.Path] != wantDiagnostics {
				t.Errorf("run %d: %s: %d structure_unrooted diagnostics, want %d", run, f.Path, diagnosed[f.Path], wantDiagnostics)
			}
		}
		for _, c := range []string{"syntax_error", "lone_cr", "too_large", "unrooted", "test_target", "build_script"} {
			if reached[c] == 0 {
				t.Errorf("run %d: no Rust file reaches case %s", run, c)
			}
		}
		if reached["unrooted"] != len(unrootedRustFiles) {
			t.Errorf("run %d: %d unrooted Rust files indexed, want %d", run, reached["unrooted"], len(unrootedRustFiles))
		}
	}
}

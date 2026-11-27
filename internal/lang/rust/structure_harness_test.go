package rust

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/scan"
)

// structureFixture is the Rust structure fixture (README in the directory).
const structureFixture = "../../../testdata/fixtures/rust-structure"

// structureExtraFiles are the fixture cases the repository cannot store as
// written (it normalises line endings to LF): the tests write them into the
// temporary copy (TestRustStructureFixture's golden included, through
// export_test.go).
var structureExtraFiles = map[string]string{
	// CRLF line endings: spans and hashes as for LF (RS-D1, RS-D2).
	"app/src/crlf.rs": "pub fn crlf() {}\r\npub struct Crlf;\r\n",
	// A lone CR: Rust reads it as white space, so the // comment hides
	// `hidden`; Graphvein reads it as a line end. Row RS-F3: nothing beyond
	// the file, counted lone_cr.
	"app/src/lonecr.rs": "// note\rpub fn hidden() {}\npub fn visible() {}\n",
	// Raw NUL bytes in a block comment and a byte string, which Rust
	// allows: the scanner reads the file as source (scan.SourceWithNUL) and
	// RS-D1 claims. (tree-sitter-rust reports an error on a NUL in a line
	// comment or a character literal: such a file is RS-F2.)
	"app/src/nul.rs": "/* a NUL: \x00 */\npub fn nul() -> &'static [u8] { b\"U3D\x00rest\" }\n",
}

// writeStructureExtraFiles writes structureExtraFiles below root.
func writeStructureExtraFiles(t *testing.T, root string) {
	t.Helper()
	for name, content := range structureExtraFiles {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// copyStructureFixture copies the fixture into a temporary directory and
// writes structureExtraFiles there.
func copyStructureFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(structureFixture)); err != nil {
		t.Fatal(err)
	}
	writeStructureExtraFiles(t, root)
	return root
}

// structureRun is one tree scanned, parsed, extracted and linked in process
// as the indexer does it.
type structureRun struct {
	files       map[string]facts.Facts
	edges       []contract.Edge
	diagnostics []contract.Diagnostic
}

// runStructure scans root and runs Extract, Refuse and Link over its Rust
// files, as internal/index does.
func runStructure(t *testing.T, root string) *structureRun {
	t.Helper()
	scanned, err := scan.Walk(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	run := &structureRun{files: map[string]facts.Facts{}}
	sources := map[string][]byte{}
	var paths []string
	var jobs []parse.Job
	loneCR := map[string]bool{}
	for _, f := range scanned.Files {
		sources[f.Record.Path] = f.Source
		if f.Record.Language == "rust" {
			loneCR[f.Record.Path] = f.LoneCR
			paths = append(paths, f.Record.Path)
			jobs = append(jobs, parse.Job{Language: "rust", Source: f.Source})
		}
	}
	options := parse.Options{Workers: 4, CacheDir: filepath.Join(t.TempDir(), "cache")}
	var recoveries []*Recovery
	var recovered []string
	err = parse.Consume(context.Background(), jobs, options, func(i int, r parse.Result) {
		if r.Err != nil {
			t.Errorf("parsing %s: %v", paths[i], r.Err)
			return
		}
		extracted := Extract(paths[i], sources[paths[i]], r.Root)
		if loneCR[paths[i]] || r.SyntaxError {
			extracted = Refuse(extracted, loneCR[paths[i]])
		}
		if !loneCR[paths[i]] && r.SyntaxError {
			if rec := PatternAttributes(r.Root, sources[paths[i]]); rec != nil {
				recoveries = append(recoveries, rec)
				recovered = append(recovered, paths[i])
			}
		}
		run.files[paths[i]] = extracted
	})
	if err != nil {
		t.Fatal(err)
	}
	// Row RS-F4, after every first tree is released, as internal/index does.
	pipeline := func(ctx context.Context, jobs []parse.Job, consume func(int, parse.Result)) error {
		return parse.Consume(ctx, jobs, options, consume)
	}
	if err := Recover(context.Background(), recoveries, pipeline, func(i int, root *parse.Node) {
		run.files[recovered[i]] = Extract(recovered[i], sources[recovered[i]], root)
	}); err != nil {
		t.Fatal(err)
	}
	_, edges, diagnostics := Link(run.files, sources)
	run.edges = edges
	run.diagnostics = diagnostics
	return run
}

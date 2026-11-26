package python

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

// structureFixture is the Python structure fixture (README in the directory).
const structureFixture = "../../../testdata/fixtures/python-structure"

// structureExtraFiles are the fixture cases the repository cannot store as
// written (it keeps UTF-8 text with LF line endings): the tests write them
// into the temporary copy (TestPythonStructureFixture's golden included,
// through export_test.go).
var structureExtraFiles = map[string]string{
	// CRLF line endings: spans and hashes as for LF (PS-D1, PS-D7).
	"crlf.py": "def crlf():\r\n    return 1\r\n\r\n\r\nCRLF_C = 1\r\n",
	// Lone CR line ends: CPython's tokenizer ends a line there too.
	"lonecr.py": "def lone():\r    return 1\r\rLONE_C = 2\r# c\rdef after():\r    pass\r",
	// A byte order mark counts in line 1's byte columns.
	"bom.py": "\xef\xbb\xbfdef bom():\n    pass\nBOM_C = 1\n",
	// A Latin-1 byte: the scanner does not read the file; Python does, so
	// `import latin1` is refused (shadowed_module), not external.
	"latin1.py": "# -*- coding: latin-1 -*-\nNAME = \"caf\xe9\"\ndef f():\n    return NAME\n",
	// An extension module beside fastext.py: FileFinder tries it first.
	"fastext.cpython-314-x86_64-linux-gnu.so": "\x7fELF\x00\x00fake",
	// Bytecode without source: Python imports it; Graphvein cannot read it.
	"onlypyc.pyc": "\xf3\x0d\x0d\x0a\x00\x00\x00\x00",
}

// structureRenames are the fixture's inert names (README): the copy gets
// the real name.
var structureRenames = map[string]string{"_build": "build"}

// writeStructureExtraFiles writes structureExtraFiles below root and
// restores the inert names.
func writeStructureExtraFiles(t *testing.T, root string) {
	t.Helper()
	for name, content := range structureExtraFiles {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for from, to := range structureRenames {
		if err := os.Rename(filepath.Join(root, from), filepath.Join(root, to)); err != nil {
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
	nodes       []contract.Node
	edges       []contract.Edge
	diagnostics []contract.Diagnostic
}

// runStructure scans root and runs Extract and LinkWithResolver over its
// Python files, as internal/index does (row PS-F2 on a tree-sitter error,
// the scanner's skipped entries for shadowed_module).
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
	for _, f := range scanned.Files {
		sources[f.Record.Path] = f.Source
		if f.Record.Language == "python" {
			paths = append(paths, f.Record.Path)
			jobs = append(jobs, parse.Job{Language: "python", Source: f.Source})
		}
	}
	options := parse.Options{Workers: 4, CacheDir: filepath.Join(t.TempDir(), "cache")}
	err = parse.Consume(context.Background(), jobs, options, func(i int, r parse.Result) {
		if r.Err != nil {
			t.Errorf("parsing %s: %v", paths[i], r.Err)
			return
		}
		extracted := Extract(paths[i], sources[paths[i]], r.Root)
		if r.SyntaxError {
			extracted = RefuseSyntaxError(extracted, paths[i])
		}
		run.files[paths[i]] = extracted
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range facts.Paths(run.files) {
		run.nodes = append(run.nodes, run.files[p].Nodes...)
		run.diagnostics = append(run.diagnostics, run.files[p].Diagnostics...)
	}
	external, edges, diagnostics := LinkWithResolver(run.files, NewModuleResolver(run.files, sources).WithSkipped(scanned.Skipped).WithDirectories(scanned.Dirs))
	run.nodes = append(run.nodes, external...)
	run.edges = edges
	run.diagnostics = append(run.diagnostics, diagnostics...)
	return run
}

// parseSource parses one Python source and returns its detached tree.
func parseSource(t *testing.T, source string) *parse.Node {
	t.Helper()
	var root *parse.Node
	err := parse.Consume(context.Background(), []parse.Job{{Language: "python", Source: []byte(source)}}, parse.Options{Workers: 1}, func(_ int, r parse.Result) {
		if r.Err != nil {
			t.Fatal(r.Err)
		}
		root = r.Root
	})
	if err != nil {
		t.Fatal(err)
	}
	return root
}

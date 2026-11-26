package golang

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

// structureFixture is the Go structure fixture (README in the directory).
const structureFixture = "../../../testdata/fixtures/go-structure"

// structureExtraFiles are the fixture cases the repository cannot store
// as written (it normalises line endings to LF): the tests write them into
// the temporary copy (TestGoStructureFixture's golden included, through
// export_test.go).
var structureExtraFiles = map[string]string{
	// CRLF line endings: spans and hashes as for LF (GS-D1, GS-D5).
	"util/crlf.go": "package util\r\n\r\nfunc CRLF() {\r\n\treturn\r\n}\r\n\r\ntype W struct {\r\n\tX int\r\n}\r\n",
	// Lone CR line endings: Go reads a lone CR as white space, so this is
	// `package util func CR() {}`, which go/parser rejects; Graphvein reads
	// it as lines. Row GS-F3: nothing beyond the file, counted lone_cr.
	"util/cr.go": "package util\r\rfunc CR() {}\r\rvar Exported = 1\r",
	// The // comment runs on past the lone CR in Go, so Go
	// sees no Hidden; GS-F3 claims neither declaration.
	"util/hidden.go": "package util\n\n// note\rfunc Hidden() {}\n\nfunc Visible() {}\n",
	// A lone-CR header in an imported directory: for Go the constraint is
	// inside the first comment line, so the file is built and names
	// package other (a MultiplePackageError). GS-F3 for the file and GS-I5
	// for imports of lonecr (imports/i.go).
	"lonecr/hdr.go": "// h\r//go:build ignore\n\npackage other\n",
	// A byte order mark before the package clause (GS-F1, GS-D1).
	"util/bom.go": "\xef\xbb\xbfpackage util\n\nfunc BOM() {}\n",
	// go/build strips the byte order mark before reading the header, so
	// the constraint excludes the file (GS-I1).
	"lib/bom_ignore.go": "\xef\xbb\xbf//go:build ignore\n\npackage lib\n\nfunc BOMIgnored() {}\n",
	// A CRLF header: the constraint excludes the file (GS-I1).
	"lib/crlf_ignore.go": "//go:build ignore\r\n\r\npackage lib\r\n\r\nfunc CRLFIgnored() {}\r\n",
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
	root    string
	goFiles []string
	sources map[string][]byte
	// loneCR are the files the scanner reports with a lone CR.
	loneCR      map[string]bool
	files       map[string]facts.Facts
	nodes       []contract.Node
	edges       []contract.Edge
	diagnostics []contract.Diagnostic
}

// runStructure scans root and runs Extract and Link over its Go files.
func runStructure(t *testing.T, root string) *structureRun {
	t.Helper()
	scanned, err := scan.Walk(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	run := &structureRun{root: root, sources: map[string][]byte{}, files: map[string]facts.Facts{}, loneCR: map[string]bool{}}
	var jobs []parse.Job
	loneCR := run.loneCR
	for _, f := range scanned.Files {
		run.sources[f.Record.Path] = f.Source
		if f.LoneCR {
			loneCR[f.Record.Path] = true
		}
		if f.Record.Language == "go" {
			run.goFiles = append(run.goFiles, f.Record.Path)
			jobs = append(jobs, parse.Job{Language: "go", Source: f.Source})
		}
	}
	options := parse.Options{Workers: 4, CacheDir: filepath.Join(t.TempDir(), "cache")}
	err = parse.Consume(context.Background(), jobs, options, func(i int, r parse.Result) {
		if r.Err != nil {
			t.Errorf("parsing %s: %v", run.goFiles[i], r.Err)
			return
		}
		extracted := Extract(run.goFiles[i], run.sources[run.goFiles[i]], r.Root)
		if loneCR[run.goFiles[i]] {
			// As the indexer does (internal/index): row GS-F3.
			extracted = RefuseLoneCR(extracted)
		}
		run.files[run.goFiles[i]] = extracted
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range facts.Paths(run.files) {
		run.nodes = append(run.nodes, run.files[p].Nodes...)
		run.diagnostics = append(run.diagnostics, run.files[p].Diagnostics...)
	}
	external, edges, diagnostics := Link(run.files, ModulePaths(run.sources))
	run.nodes = append(run.nodes, external...)
	run.edges = edges
	run.diagnostics = append(run.diagnostics, diagnostics...)
	return run
}

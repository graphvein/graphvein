package query

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
)

func TestPresenterFileState(t *testing.T) {
	root := t.TempDir()
	source := []byte("a\r\nb\n")
	if err := os.WriteFile(filepath.Join(root, "a.go"), source, 0o644); err != nil {
		t.Fatal(err)
	}
	p := NewPresenter(root, config.DefaultIDE())
	stored := contract.ContentHash(source)
	if got := p.FileState("a.go", stored); got != FileUnchanged {
		t.Fatalf("unchanged: %s", got)
	}
	if got := p.FileState("a.go", contract.ContentHash([]byte("old"))); got != FileChanged {
		t.Fatalf("changed: %s", got)
	}
	if got := p.FileState("gone.go", stored); got != FileMissing {
		t.Fatalf("missing: %s", got)
	}
	// One read per file: a later edit is not seen by the same presenter, so
	// its link and its state describe the same bytes.
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	span := contract.Span{StartLine: 2, StartColumn: 0, EndLine: 2, EndColumn: 1}
	n := p.Node(contract.Node{File: "a.go", Span: &span}, stored)
	if p.FileState("a.go", stored) != FileUnchanged || n.IDE == nil || !n.IDE.Exact {
		t.Fatalf("second read: %+v", n.IDE)
	}
}

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/scan"
	"github.com/graphvein/graphvein/internal/store"
)

func TestIndexInvalidRecordExitZero(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	t.Setenv("GRAPHVEIN_SQLITE_PATH", filepath.Join(t.TempDir(), "index.db"))
	if err := os.WriteFile(filepath.Join(root, "bad.ts"), []byte("export function bad() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "good.ts"), []byte("export function good() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Inject malformed cached extractor output through a test callback only.
	emitter := func(ctx context.Context, root string, s store.Store, output io.Writer, opt index.Options) (index.Summary, error) {
		scanned, err := scan.Walk(ctx, root)
		if err != nil {
			return index.Summary{}, err
		}
		for _, f := range scanned.Files {
			if f.Record.Path != "bad.ts" {
				continue
			}
			span := contract.Span{StartLine: 1, EndLine: 1}
			out := facts.Facts{Exports: map[string]string{}, Nodes: []contract.Node{{Type: "node", Kind: "function", Identity: "function:bad.ts#bad", QualifiedName: "bad", File: "bad.ts", Level: 4, Span: &span, ContentHash: f.Record.ContentHash, Attributes: map[string]any{}}}}
			data, err := json.Marshal(out)
			if err != nil {
				return index.Summary{}, err
			}
			if err := s.SaveFactCache(ctx, opt.Repository, f.Record.Path, f.Record.ContentHash, index.Version+"/max=2097152", data); err != nil {
				return index.Summary{}, err
			}
		}
		return index.Emit(ctx, root, s, output, opt)
	}
	var stdout, stderr bytes.Buffer
	if code := indexCommandWithEmitter("index", []string{root, "--repository", "fixture"}, &stdout, &stderr, emitter); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, &stderr)
	}
	if stderr.Len() != 0 || !strings.Contains(stdout.String(), "dropped: 1") {
		t.Fatal(stdout.String(), stderr.String())
	}
}

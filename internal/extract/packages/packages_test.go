package packages_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/packages"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

const (
	fixture  = "../../../testdata/fixtures/components"
	taskPath = "../../../docs/contract/claims/architecture-graph.md"
)

// fixtureRoot copies the components fixture and restores its inert names
// (README).
func fixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(fixture)); err != nil {
		t.Fatal(err)
	}
	for from, to := range map[string]string{"_graphvein.yaml": "graphvein.yaml", "crates/app/_tauri.conf.json": "crates/app/tauri.conf.json"} {
		if err := os.Rename(filepath.Join(root, filepath.FromSlash(from)), filepath.Join(root, filepath.FromSlash(to))); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// projection is what the golden keeps: the package nodes and the
// component_manifest_unread diagnostics.
type projection struct {
	Nodes       []contract.Node
	Diagnostics []contract.Diagnostic
}

// TestComponentsFixture indexes the fixture through the indexer, checks
// the package nodes and CM-U1 diagnostics against the golden, then replays
// warm (nothing parsed, the same stream).
func TestComponentsFixture(t *testing.T) {
	ctx := context.Background()
	root := fixtureRoot(t)
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	var mu sync.Mutex
	tags := map[string]bool{}
	packages.SetClaimTag(func(row string) {
		mu.Lock()
		tags[row] = true
		mu.Unlock()
	})
	t.Cleanup(func() { packages.SetClaimTag(nil) })
	opt := index.Options{Repository: "fixture", GeneratedAt: "2026-11-18T00:00:00Z", Parser: parse.Options{Workers: 2, CacheDir: filepath.Join(t.TempDir(), "cache")}}
	var first []byte
	for run := 0; run < 2; run++ {
		var stream bytes.Buffer
		summary, err := index.Emit(ctx, root, s, &stream, opt)
		if err != nil {
			t.Fatal(err)
		}
		data := append([]byte(nil), stream.Bytes()...)
		if _, err := ingest.StreamWithWrites(ctx, &stream, s); err != nil {
			t.Fatal(err)
		}
		if run == 1 && (!bytes.Equal(first, data) || summary.Parsed != 0) {
			t.Fatalf("replay parsed %d files or changed the stream", summary.Parsed)
		}
		first = data
		if run == 0 {
			snapshot, err := s.Dump(ctx, "fixture")
			if err != nil {
				t.Fatal(err)
			}
			golden.Check(t, fixture+".golden.json", project(snapshot))
		}
	}
	reached := map[string]bool{}
	for tag := range tags {
		row, _, _ := strings.Cut(tag, "/")
		reached[row] = true
	}
	for _, r := range packages.Contract {
		if !reached[r.ID] {
			t.Errorf("row %s is not reached by the fixture", r.ID)
		}
	}
	for _, form := range []string{"CM-M1/none", "CM-M3/none", "CM-M5/none", "CM-M5/enclosed", "CM-W1/npm", "CM-W1/cargo", "CM-W1/uv"} {
		if !tags[form] {
			t.Errorf("form %s is not reached by the fixture", form)
		}
	}
}

func project(s store.Snapshot) projection {
	p := projection{Nodes: []contract.Node{}, Diagnostics: []contract.Diagnostic{}}
	for _, n := range s.Nodes {
		if n.Kind == "package" {
			p.Nodes = append(p.Nodes, n)
		}
	}
	for _, d := range s.Diagnostics {
		if d.Code == packages.UnreadCode {
			p.Diagnostics = append(p.Diagnostics, d)
		}
	}
	return p
}

// TestPackagesContractRows keeps the indexer rows equal to the first rows
// of the task file's components table, in order (the server's test checks
// the whole table).
func TestPackagesContractRows(t *testing.T) {
	data, err := os.ReadFile(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "\n## Claim contract: components")
	if start < 0 {
		t.Fatal("no components table")
	}
	var ids []string
	for _, line := range strings.Split(text[start+1:], "\n")[1:] {
		if strings.HasPrefix(line, "## ") {
			break
		}
		if strings.HasPrefix(line, "| CM-") && !strings.HasPrefix(line, "| CM-A") && !strings.HasPrefix(line, "| CM-R") {
			ids = append(ids, strings.TrimSpace(strings.Split(line, "|")[1]))
		}
	}
	var code []string
	for _, r := range packages.Contract {
		code = append(code, r.ID)
		if r.Form == "" {
			t.Errorf("row %s names no form", r.ID)
		}
	}
	if !reflect.DeepEqual(ids, code) {
		t.Errorf("task rows %v, code rows %v", ids, code)
	}
}

// TestUnreadIsolation checks that a refused manifest affects nothing else
// and that each reader's refusals are CM-U1 diagnostics.
func TestUnreadIsolation(t *testing.T) {
	nodes, diags := packages.Link(packages.Input{Sources: map[string][]byte{
		"a/package.json":   []byte(`{"name": 1}`),
		"b/package.json":   []byte(`[]`),
		"c/Cargo.toml":     []byte("[package]\nversion = \"1\"\n"),
		"d/Cargo.toml":     []byte("[dependencies]\n"),
		"e/Cargo.toml":     []byte("[package\n"),
		"f/pyproject.toml": []byte("[project]\nversion = \"1\"\n"),
		"g/setup.py":       []byte("from setuptools import setup\nsetup(name='g')\n"),
		"h/package.json":   []byte(`{"name": "h"}`),
	}})
	if len(nodes) != 1 || nodes[0].Identity != "package:h/package.json#h" {
		t.Errorf("nodes %v", nodes)
	}
	var files []string
	for _, d := range diags {
		if d.Code != packages.UnreadCode || d.Severity != "info" {
			t.Errorf("diagnostic %v", d)
		}
		files = append(files, d.File)
	}
	// g/setup.py has no Python facts: not read as Python, so unread.
	want := []string{"a/package.json", "b/package.json", "c/Cargo.toml", "d/Cargo.toml", "e/Cargo.toml", "f/pyproject.toml", "g/setup.py"}
	if !reflect.DeepEqual(files, want) {
		t.Errorf("unread %v, want %v", files, want)
	}
}

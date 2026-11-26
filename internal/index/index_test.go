package index_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

func options(t *testing.T) index.Options {
	return index.Options{Repository: "fixture", GeneratedAt: "2026-10-05T00:00:00Z", Parser: parse.Options{Workers: 4, CacheDir: filepath.Join(t.TempDir(), "wazero")}}
}
func open(t *testing.T) store.Store {
	t.Helper()
	s, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func emit(t *testing.T, root string, s store.Store, opt index.Options) ([]byte, index.Summary, store.Writes) {
	t.Helper()
	var b bytes.Buffer
	summary, err := index.Emit(context.Background(), root, s, &b, opt)
	if err != nil {
		t.Fatal(err)
	}
	writes, err := ingest.StreamWithWrites(context.Background(), bytes.NewReader(b.Bytes()), s)
	if err != nil {
		t.Fatal(err)
	}
	return b.Bytes(), summary, writes
}
func TestRepositoryGolden(t *testing.T) {
	s := open(t)
	opt := options(t)
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/repos/typescript")); err != nil {
		t.Fatal(err)
	}
	for _, names := range [][2]string{{"_graphveinignore", ".graphveinignore"}, {"_claude", ".claude"}, {"nested/_gitignore", "nested/.gitignore"}, {"nested/_skip.ts", "nested/skip.ts"}} {
		if err := os.Rename(filepath.Join(root, names[0]), filepath.Join(root, names[1])); err != nil {
			t.Fatal(err)
		}
	}
	data, summary, _ := emit(t, root, s, opt)
	var records []any
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var v any
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.UseNumber()
		if err := decoder.Decode(&v); err != nil {
			t.Fatal(err)
		}
		records = append(records, v)
	}
	golden.Check(t, "../../testdata/repos/typescript.golden.json", records)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/repos/typescript.store.golden.json", snapshot)
	again, reused, writes := emit(t, root, s, opt)
	if !bytes.Equal(data, again) || reused.Parsed != 0 || reused.Reused != summary.Parsed || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0 {
		t.Fatalf("cache replay differed: %+v %+v", reused, writes)
	}
	if summary.Diagnostics == 0 {
		t.Fatal("missing syntax/shadow diagnostics")
	}
}
func TestIncrementalRelinking(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.ts")
	b := filepath.Join(root, "b.ts")
	os.WriteFile(a, []byte("export function target() { return 1; }\n"), 0644)
	os.WriteFile(b, []byte("import { target } from './a';\nexport function caller() { return target(); }\n"), 0644)
	s := open(t)
	opt := options(t)
	emit(t, root, s, opt)
	os.WriteFile(a, []byte("export function target() { return 2; }\n"), 0644)
	_, summary, writes := emit(t, root, s, opt)
	if summary.Parsed != 1 || summary.Reused != 1 || writes.Files != 1 || writes.Nodes != 2 || writes.Edges != 0 {
		t.Fatalf("incremental work: %+v %+v", summary, writes)
	}
	incremental, _ := s.Dump(context.Background(), "fixture")
	full := open(t)
	emit(t, root, full, opt)
	complete, _ := full.Dump(context.Background(), "fixture")
	if !reflect.DeepEqual(incremental, complete) {
		t.Fatal("incremental snapshot differs from full index")
	}
	changed, err := index.Freshness(context.Background(), root, "fixture", s)
	if err != nil || len(changed) != 0 {
		t.Fatal(changed, err)
	}
	os.WriteFile(a, []byte("export function replacement() { return 2; }\n"), 0644)
	changed, err = index.Freshness(context.Background(), root, "fixture", s)
	if err != nil || !reflect.DeepEqual(changed, []string{"a.ts"}) {
		t.Fatal(changed, err)
	}
	_, summary, _ = emit(t, root, s, opt)
	if summary.Parsed != 1 || summary.Reused != 1 {
		t.Fatal(summary)
	}
	snapshot, _ := s.Dump(context.Background(), "fixture")
	for _, e := range snapshot.Edges {
		if e.Kind == "calls" {
			t.Fatal("stale target edge survived")
		}
	}
	os.Remove(a)
	emit(t, root, s, opt)
	snapshot, _ = s.Dump(context.Background(), "fixture")
	for _, n := range snapshot.Nodes {
		if n.File == "a.ts" {
			t.Fatal("deleted file survived")
		}
	}
	opt.ExtractorVersion = "new-version"
	_, summary, _ = emit(t, root, s, opt)
	if summary.Parsed != 1 || summary.Reused != 0 {
		t.Fatal("extractor version failed to invalidate", summary)
	}
}
func TestOversizeAndCacheCorruption(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "large.ts"), []byte(strings.Repeat(" ", 2<<20)+"export const large = () => 1;"), 0644)
	s := open(t)
	opt := options(t)
	_, summary, _ := emit(t, root, s, opt)
	if summary.Parsed != 0 || summary.Nodes != 1 || summary.Diagnostics != 1 {
		t.Fatal(summary)
	}
	oversize, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/repos/oversize.store.golden.json", oversize)
	opt.MaxFileSize = 3 << 20
	_, summary, _ = emit(t, root, s, opt)
	if summary.Parsed != 1 {
		t.Fatal(summary)
	}
	snapshot, _ := s.Dump(context.Background(), "fixture")
	s.SaveFactCache(context.Background(), "fixture", "large.ts", snapshot.Files[0].ContentHash, index.Version+"/max=3145728", []byte("invalid"))
	_, summary, _ = emit(t, root, s, opt)
	if summary.Parsed != 1 {
		t.Fatal("corrupt cache reused", summary)
	}
}

package index_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

func moduleSnapshot(t *testing.T, snapshot store.Snapshot) map[string][]string {
	t.Helper()
	out := map[string][]string{"imports": {}, "calls": {}, "routes": {}, "diagnostics": {}}
	for _, e := range snapshot.Edges {
		if e.Kind == "imports" || e.Kind == "calls" {
			out[e.Kind] = append(out[e.Kind], e.Source+" -> "+e.Target)
		}
	}
	for _, n := range snapshot.Nodes {
		if n.Kind == "route" {
			var paths []string
			for _, p := range routePaths(t, n) {
				paths = append(paths, p["template"].(string))
			}
			handler, _ := n.Attributes["handler"].(string)
			out["routes"] = append(out["routes"], n.Identity+" | "+handler+" | "+strings.Join(paths, ","))
		}
	}
	for _, d := range snapshot.Diagnostics {
		if d.Code == "extract_invalid_record" || d.Code == "extract_failed" || d.Code == "parse_failed" || d.Code == "syntax_error" {
			t.Fatal(d)
		}
		out["diagnostics"] = append(out["diagnostics"], d.File+": "+d.Code)
	}
	for key := range out {
		sort.Strings(out[key])
	}
	return out
}

func TestModuleResolutionGoldenAndMetadataReplay(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/module-resolution")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	before, first, _ := emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/module-resolution.golden.json", moduleSnapshot(t, snapshot))
	again, warm, writes := emit(t, root, s, opt)
	if string(before) != string(again) || warm.Parsed != 0 || warm.Reused != first.Parsed || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0 {
		t.Fatalf("warm replay: %+v %+v", warm, writes)
	}
	config := filepath.Join(root, "tsconfig.json")
	data, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte(strings.ReplaceAll(string(data), "aliased/*", "alternate/*")), 0644); err != nil {
		t.Fatal(err)
	}
	_, changed, _ := emit(t, root, s, opt)
	if changed.Parsed != 0 || changed.Reused != first.Parsed {
		t.Fatalf("metadata edit reparsed source: %+v", changed)
	}
	after, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range after.Edges {
		if e.Kind == "calls" && e.Source == "function:consumer.ts#pathsAlias" {
			found = true
			if e.Target != "function:alternate/tool.ts#work" {
				t.Fatal("stale alias", e)
			}
		}
	}
	if !found {
		t.Fatal("alias call edge disappeared")
	}
	fresh := open(t)
	emit(t, root, fresh, opt)
	complete, err := fresh.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, complete) {
		t.Fatal("metadata replay differs from a full index")
	}
	// package.json is also live metadata; switch the entry without parsing sources.
	packageFile := filepath.Join(root, "packages/condition/package.json")
	if err := os.WriteFile(packageFile, []byte(`{"name":"@condition","main":"./wrong.ts"}`), 0644); err != nil {
		t.Fatal(err)
	}
	_, changed, _ = emit(t, root, s, opt)
	if changed.Parsed != 0 {
		t.Fatal("package edit reparsed source", changed)
	}
	after, err = s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, e := range after.Edges {
		if e.Kind == "calls" && e.Source == "function:consumer.ts#conditions" {
			found = true
			if e.Target != "function:packages/condition/wrong.ts#work" {
				t.Fatal("stale package entry", e)
			}
		}
	}
	if !found {
		t.Fatal("package call edge disappeared")
	}
}

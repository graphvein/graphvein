package index_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
)

func TestFastAPIHelperGoldenAndReplay(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/fastapi-helpers")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	_, _, _ = emit(t, root, s, opt)
	first, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	routes := routeSnapshot(first)
	if len(routes.Nodes) != 12 {
		t.Fatalf("routes=%d, want 12", len(routes.Nodes))
	}
	wanted := map[string]map[string]bool{
		"GET ":  {"/first/included/child": true, "/second/included/child": true, "/first/argument/child": false, "/second/argument/child": true, "/first/local/deep/child": false},
		"GET /": {"/first/included/child": true, "/second/included/child": true, "/first/argument/child": false, "/second/argument/child": true, "/first/local/deep/child": false},
	}
	for _, n := range routes.Nodes {
		paths := routePaths(t, n)
		expected := wanted[n.QualifiedName]
		if n.Attributes["handler"] == "function:children.py#async_handler" {
			expected = map[string]bool{"/async/x": false, "/second/local/deep/async/x": false}
		}
		if n.Attributes["handler"] == "function:children.py#sub_handler" {
			// The mounted sub-app answers only below its mount (T035).
			expected = map[string]bool{"/mounted/sub": false}
		}
		if len(paths) != len(expected) {
			t.Fatalf("%s: paths=%v want=%v", n.Identity, paths, expected)
		}
		for _, p := range paths {
			template := p["template"].(string)
			condition, ok := expected[template]
			if !ok || p["conditional"] != condition || p["resolution"] != "literal" {
				t.Fatalf("%s: path=%v want=%v", n.Identity, p, expected)
			}
			_, slash := p["trailing_slash"]
			if want := strings.HasSuffix(n.Attributes["pattern"].(string), "/"); slash != want {
				t.Fatalf("%s: slash flag=%v want=%v", n.Identity, slash, want)
			}
		}
	}
	golden.Check(t, "../../testdata/fixtures/fastapi-helpers.golden.json", routes)
	_, replay, _ := emit(t, root, s, opt)
	if replay.Parsed != 0 {
		t.Fatalf("replay parsed=%d", replay.Parsed)
	}
	after, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(after)
	if string(a) != string(b) {
		t.Fatal("cached replay differs")
	}
	p := filepath.Join(root, "main.py")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "install(first)", "install(second)", 1))
	if err := os.WriteFile(p, data, 0600); err != nil {
		t.Fatal(err)
	}
	_, incremental, _ := emit(t, root, s, opt)
	if incremental.Parsed != 1 {
		t.Fatalf("target-only edit parsed=%d", incremental.Parsed)
	}
	changed, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range routeSnapshot(changed).Nodes {
		for _, p := range routePaths(t, n) {
			if p["template"] == "/first/included/child" {
				t.Fatal("stale helper path", n.Identity)
			}
		}
	}
	freshStore := open(t)
	emit(t, root, freshStore, opt)
	fresh, err := freshStore.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	a, _ = json.Marshal(changed)
	b, _ = json.Marshal(fresh)
	if string(a) != string(b) {
		t.Fatal("target-only replay differs from fresh")
	}
}

package index_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

func routeSnapshot(s store.Snapshot) store.Snapshot {
	out := store.Snapshot{}
	for _, n := range s.Nodes {
		if n.Kind == "route" {
			out.Nodes = append(out.Nodes, n)
		}
	}
	for _, e := range s.Edges {
		if e.Kind == "exposes_route" || (e.Kind == "contains" && strings.HasPrefix(e.Target, "route:")) {
			out.Edges = append(out.Edges, e)
		}
	}
	for _, d := range s.Diagnostics {
		if strings.HasPrefix(d.Code, "route_") {
			out.Diagnostics = append(out.Diagnostics, d)
		}
	}
	return out
}

func routePaths(t *testing.T, n contract.Node) []map[string]any {
	t.Helper()
	data, err := json.Marshal(n.Attributes["paths"])
	if err != nil {
		t.Fatal(err)
	}
	var paths []map[string]any
	if err := json.Unmarshal(data, &paths); err != nil {
		t.Fatal(err)
	}
	return paths
}

func TestExpressGoldenAndMountReplay(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/express")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	data, first, _ := emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	routes := routeSnapshot(snapshot)
	if len(routes.Nodes) != 20 {
		for _, n := range routes.Nodes {
			t.Log(n.Identity, n.Attributes)
		}
		for _, d := range snapshot.Diagnostics {
			t.Log(d)
		}
		t.Fatalf("route count %d", len(routes.Nodes))
	}
	for _, d := range snapshot.Diagnostics {
		if d.Code == "extract_invalid_record" || d.Code == "parse_failed" || d.Code == "extract_failed" {
			t.Fatal(d)
		}
	}
	for _, n := range routes.Nodes {
		if n.File == "router.ts" && n.QualifiedName == "GET /barrel" && n.Attributes["handler"] != "function:handlers.ts#hidden" {
			t.Fatal("barrel handler did not resolve", n)
		}
		if n.QualifiedName == "GET /not-resolved-through-reexport" {
			if n.Attributes["handler"] != "function:handlers.ts#handle" || len(routePaths(t, n)) != 0 {
				t.Fatal("relayed factory should produce an unmounted route", n)
			}
		}
		if strings.Contains(n.QualifiedName, "fake") || strings.Contains(n.QualifiedName, "not-express") || strings.Contains(n.QualifiedName, "api/x") {
			t.Fatal("false route", n)
		}
		if n.QualifiedName == "GET /default" {
			paths := routePaths(t, n)
			if len(paths) != 1 || paths[0]["template"] != "/returned/default" {
				t.Fatal("root paths were not deduplicated", paths)
			}
		}
		if n.QualifiedName == "GET /ambiguous" && n.Attributes["handler"] != nil {
			t.Fatal("ambiguous handler resolved", n)
		}
		if n.File == "bindings.ts" && n.QualifiedName == "GET /repeat" && n.Attributes["handler_inline"] != true {
			t.Fatal("duplicate order lost outer registration", n)
		}
		if n.File == "bindings.ts" && n.QualifiedName == "GET /repeat@2" && n.Attributes["handler"] != "function:handlers.ts#handle" {
			t.Fatal("duplicate order lost inner registration", n)
		}
		source, err := os.ReadFile(filepath.Join(root, n.File))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(source), "\n")
		offset := func(line, column int) int {
			total := column
			for _, text := range lines[:line-1] {
				total += len(text) + 1
			}
			return total
		}
		span := n.Span
		start, end := offset(span.StartLine, span.StartColumn), offset(span.EndLine, span.EndColumn)
		if n.ContentHash != contract.ContentHash(source[start:end]) {
			t.Fatal("route hash does not cover registration span", n)
		}
	}
	golden.Check(t, "../../testdata/fixtures/express.golden.json", routes)
	again, warm, _ := emit(t, root, s, opt)
	if string(data) != string(again) || warm.Parsed != 0 || warm.Reused != first.Parsed {
		t.Fatal("warm replay", warm)
	}
	app := filepath.Join(root, "app.ts")
	source, err := os.ReadFile(app)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(app, []byte(strings.ReplaceAll(string(source), "'/second'", "'/changed'")), 0644); err != nil {
		t.Fatal(err)
	}
	_, changed, _ := emit(t, root, s, opt)
	if changed.Parsed != 1 || changed.Reused != first.Parsed-1 {
		t.Fatal(changed)
	}
	after, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, n := range after.Nodes {
		if n.Kind == "route" && n.File == "router.ts" {
			for _, p := range routePaths(t, n) {
				template := p["template"].(string)
				if strings.HasPrefix(template, "/second/") {
					t.Fatal("stale path", template)
				}
				if strings.HasPrefix(template, "/changed/") {
					seen = true
				}
			}
		}
	}
	if !seen {
		t.Fatal("unchanged router was not recomposed")
	}
	full := open(t)
	emit(t, root, full, opt)
	fresh, err := full.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(after)
	b, _ := json.Marshal(fresh)
	if string(a) != string(b) {
		t.Fatal("relinked snapshot differs from fresh")
	}
}

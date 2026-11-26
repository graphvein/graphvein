package index_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

func operationRouteSnapshot(snapshot store.Snapshot) store.Snapshot {
	out := apiSnapshot(snapshot)
	for _, n := range snapshot.Nodes {
		if n.Kind == "route" {
			out.Nodes = append(out.Nodes, n)
		}
	}
	for _, e := range snapshot.Edges {
		if e.Kind == "documents_route" {
			out.Edges = append(out.Edges, e)
		}
	}
	return out
}

func TestOpenAPIRouteGoldenAndReplay(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/openapi-routes")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "_graphvein.yaml"), filepath.Join(root, "graphvein.yaml")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	_, _, _ = emit(t, root, s, opt)
	first, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/openapi-routes.golden.json", operationRouteSnapshot(first))
	statuses, edges, coverage := map[string]int{}, 0, 0
	operations := map[string]contract.Node{}
	links := map[string][]contract.Edge{}
	for _, n := range first.Nodes {
		if n.Kind == "api_operation" {
			statuses[n.Attributes["status"].(string)]++
			operations[n.File+"#"+n.QualifiedName] = n
		}
	}
	for _, e := range first.Edges {
		if e.Kind == "documents_route" {
			edges++
			links[e.Source] = append(links[e.Source], e)
			if e.Provenance != "inferred" || len(e.Evidence) != 1 {
				t.Fatal("invalid inference evidence", e)
			}
		}
	}
	for _, d := range first.Diagnostics {
		if d.Code == "openapi_coverage" {
			coverage++
		}
	}
	if !reflect.DeepEqual(statuses, map[string]int{"linked": 17, "unmatched": 4, "ambiguous": 1, "test_only": 1, "unresolved": 1}) || edges != 18 || coverage != 3 {
		t.Fatal(statuses, edges, coverage)
	}
	for _, tc := range []struct {
		file, name, target string
		confidence         float64
	}{
		{"openapi.json", "GET /t013b/relative", "route:routes.ts#GET%20%2Ft013b%2Frelative", 0.9},
		{"openapi.json", "GET /t013b/absolute", "route:routes.ts#GET%20%2Ft013b%2Fabsolute", 0.7},
		{"openapi.json", "HEAD /t013b/head", "route:routes.ts#GET%20%2Ft013b%2Fhead", 0.9},
		{"openapi.json", "QUERY /t013b/any", "route:routes.ts#ANY%20%2Ft013b%2Fany", 0.9},
		{"openapi.json", "GET /t013b/duplicate", "route:routes.ts#GET%20%2Ft013b%2Fduplicate", 0.9},
		{"openapi.json", "GET /t013b/slash", "route:twins.py#GET%20%2Ft013b%2Fslash", 0.9},
		{"openapi.json", "GET /t013b/slash/", "route:twins.py#GET%20%2Ft013b%2Fslash%2F", 0.9},
		{"configured-openapi.json", "GET /repaired", "route:routes.ts#GET%20%2Ft013b%2Frepaired", 0.9},
		{"configured-openapi.json", "GET /overridden", "route:routes.ts#GET%20%2Ft013b%2Foverridden", 0.9},
	} {
		n := operations[tc.file+"#"+tc.name]
		got := links[n.Identity]
		if len(got) != 1 || got[0].Target != tc.target || got[0].Confidence != tc.confidence || !reflect.DeepEqual(got[0].Evidence[0].Span, *n.Span) {
			t.Fatal(tc, got)
		}
	}
	lowercase := operations["openapi.json#query /t013b/any"]
	if lowercase.Attributes["status"] != "unmatched" || len(links[lowercase.Identity]) != 0 {
		t.Fatal("Express rejects lowercase methods before routing", lowercase, links[lowercase.Identity])
	}
	// An unchanged language cache must reflect configuration-only relinking.
	configPath := filepath.Join(root, "graphvein.yaml")
	if err := os.WriteFile(configPath, []byte("version: 1\nopenapi:\n  base_paths:\n    '.': /wrong\n    configured-openapi.json: /t013b\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, replay, _ := emit(t, root, s, opt)
	if replay.Parsed != 0 || replay.Reused != 3 {
		t.Fatal(replay)
	}
	after, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range after.Nodes {
		if n.Kind != "api_operation" {
			continue
		}
		if n.File == "configured-openapi.json" && n.Attributes["status"] != "linked" || n.File != "configured-openapi.json" && n.Attributes["status"] != "unmatched" {
			t.Fatal("longest configured prefix", n)
		}
	}
	if err := os.WriteFile(configPath, []byte("version: 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, replay, _ = emit(t, root, s, opt)
	if replay.Parsed != 0 {
		t.Fatal(replay)
	}
	after, err = s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range after.Nodes {
		if n.Kind == "api_operation" && n.File == "configured-openapi.json" {
			want := "unresolved"
			if strings.Contains(n.QualifiedName, "overridden") {
				want = "unmatched"
			}
			if n.Attributes["status"] != want {
				t.Fatal(n)
			}
		}
	}
}

// Canonical attributes must not acquire client-only matching scratch fields.
func TestOpenAPIRouteAttributes(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/openapi-routes")); err != nil {
		t.Fatal(err)
	}
	s := open(t)
	emit(t, root, s, options(t))
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range snapshot.Nodes {
		if n.Kind != "api_operation" {
			continue
		}
		data, _ := json.Marshal(n.Attributes["paths"])
		var paths []map[string]any
		if err := json.Unmarshal(data, &paths); err != nil {
			t.Fatal(err)
		}
		for _, p := range paths {
			for _, key := range []string{"method", "scope", "base", "path_template", "origin"} {
				if _, exists := p[key]; exists {
					t.Fatal("client scratch field", key, p)
				}
			}
		}
	}
}

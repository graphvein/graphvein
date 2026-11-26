package index_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
)

// buildConstantRows are the rows of the build-time constant base table in
// docs/extract/clients.md ("Build-time constant bases"). Every modelled
// form maps to exactly one row; BC-U is the single unknown default.
var buildConstantRows = []string{"BC-1", "BC-2", "BC-3", "BC-4", "BC-U"}

// TestBuildConstantBases checks the build-constants fixture: each client
// call against its claim row (independent of the golden), the golden of
// every client call and calls_route edge, and that the table rows in the
// docs are exactly the rows the fixture covers.
func TestBuildConstantBases(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/build-constants")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	type want struct {
		row, base, expression, status string
		confidence                    float64
	}
	// expression "" means no base_expression; confidence 0 means no edge.
	expected := map[string]want{
		"frontend.ts:1":    {"BC-1", "unresolved", "__API_URL__", "linked", 0.9},
		"frontend.ts:2":    {"BC-1", "unresolved", "__API_URL__", "wrapper", 0},
		"frontend.ts:3":    {"BC-1", "unresolved", "__API_URL__", "linked", 0.9},
		"frontend.ts:4":    {"BC-1", "unresolved", "__GLOBAL_API__", "linked", 0.9},
		"frontend.ts:5":    {"BC-U", "unresolved", "", "dynamic", 0},
		"frontend.ts:6":    {"BC-U", "unresolved", "", "dynamic", 0},
		"frontend.ts:7":    {"BC-U", "unresolved", "", "dynamic", 0},
		"frontend.ts:8":    {"BC-U", "unresolved", "", "dynamic", 0},
		"frontend.ts:9":    {"BC-U", "unresolved", "", "dynamic", 0},
		"frontend.ts:10":   {"BC-U", "unresolved", "", "dynamic", 0},
		"frontend.ts:11":   {"BC-U", "unresolved", "", "dynamic", 0},
		"frontend.ts:12":   {"BC-1", "unresolved", "__API_URL__", "unmatched", 0},
		"url-objects.ts:3": {"BC-2", "unresolved", "__API_URL__", "linked", 0.7},
		"url-objects.ts:7": {"BC-2", "unresolved", "import.meta.env.VITE_API", "linked", 0.7},
		// The unresolved-base anchor guard applies to origin bases too
		// (conservative, as for merged alternatives in ADR 0018).
		"url-objects.ts:11": {"BC-2", "unresolved", "__API_URL__", "unmatched", 0},
		"url-objects.ts:18": {"BC-3", "unresolved", "__API_URL__", "linked", 0.7},
		"url-objects.ts:24": {"BC-U", "none", "", "linked", 0.7},
		"url-objects.ts:32": {"BC-U", "none", "", "linked", 0.7},
		"url-objects.ts:40": {"BC-U", "none", "", "linked", 0.7},
		"url-objects.ts:44": {"BC-U", "unresolved", "", "dynamic", 0},
		"orval/app.ts:2":    {"BC-4", "unresolved", "__API_URL__", "linked", 0.7},
	}
	edges := map[string][]contract.Edge{}
	var projected []any
	for _, e := range snapshot.Edges {
		if e.Kind == "calls_route" {
			edges[e.Source] = append(edges[e.Source], e)
			projected = append(projected, e)
		}
	}
	covered := map[string]bool{}
	seen := map[string]bool{}
	for _, n := range snapshot.Nodes {
		if n.Kind != "client_call" || n.Attributes["library"] == "fetch" && n.File == "orval/transport.ts" {
			continue
		}
		projected = append(projected, n)
		key := fmt.Sprintf("%s:%d", n.File, n.Span.StartLine)
		w, ok := expected[key]
		if !ok {
			t.Errorf("%s: call without a row (%s)", key, n.Identity)
			continue
		}
		seen[key] = true
		covered[w.row] = true
		expression, _ := n.Attributes["base_expression"].(string)
		if n.Attributes["base"] != w.base || expression != w.expression || n.Attributes["status"] != w.status {
			t.Errorf("%s (%s): base %v %q status %v, want %s %q %s", key, w.row, n.Attributes["base"], expression, n.Attributes["status"], w.base, w.expression, w.status)
		}
		got := 0.0
		if len(edges[n.Identity]) == 1 {
			got = edges[n.Identity][0].Confidence
		}
		if len(edges[n.Identity]) > 1 || got != w.confidence {
			t.Errorf("%s (%s): edges %+v, want confidence %v", key, w.row, edges[n.Identity], w.confidence)
		}
	}
	for key := range expected {
		if !seen[key] {
			t.Errorf("%s: expected call not emitted", key)
		}
	}
	docs, err := os.ReadFile("../../docs/extract/clients.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range buildConstantRows {
		if !covered[row] {
			t.Errorf("row %s has no fixture call", row)
		}
		if !strings.Contains(string(docs), "| "+row+" |") {
			t.Errorf("row %s is not in docs/extract/clients.md", row)
		}
	}
	if n := strings.Count(string(docs), "| BC-"); n != len(buildConstantRows) {
		t.Errorf("docs/extract/clients.md has %d BC rows, the test knows %d", n, len(buildConstantRows))
	}
	golden.Check(t, "../../testdata/fixtures/build-constants.golden.json", projected)
}

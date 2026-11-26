package index_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/extract/openapi"
	"github.com/graphvein/graphvein/internal/extract/routes"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

// specShapeSnapshot renders every route shape of a snapshot with its
// security requirements, schemes and models, which routes.ShapeLines
// leaves out: the mapped part of a shape lives there too (T029 SH-O).
func specShapeSnapshot(t *testing.T, s store.Snapshot) map[string][]string {
	t.Helper()
	out := map[string][]string{"shapes": {}, "diagnostics": {}}
	var ids []string
	lines := map[string][]string{}
	for _, n := range s.Nodes {
		if n.Kind != "route" {
			continue
		}
		ids = append(ids, n.Identity)
		v, ok := n.Attributes[routeshape.AttributeKey]
		if !ok {
			lines[n.Identity] = []string{unescape(n.Identity) + ": no shape"}
			continue
		}
		shape, err := routeshape.FromAttribute(v)
		if err != nil {
			t.Fatalf("%s: %v", n.Identity, err)
		}
		l := routes.ShapeLines(unescape(n.Identity), shape)
		for _, r := range shape.Security.Items {
			names := make([]string, len(r.Schemes))
			for i, u := range r.Schemes {
				names[i] = u.Name + "[" + strings.Join(u.Scopes, " ") + "]"
			}
			l = append(l, fmt.Sprintf("    requirement {%s} | %s %s %s:%d %.1f", strings.Join(names, ", "), r.Provenance.Kind, r.Provenance.Rule, r.Provenance.File, r.Provenance.Line, r.Provenance.Confidence))
		}
		for _, sc := range shape.Schemes {
			data, _ := json.Marshal(sc)
			l = append(l, "  scheme "+string(data))
		}
		for _, m := range shape.Models {
			data, _ := json.Marshal(m.Schema)
			l = append(l, fmt.Sprintf("  model %s %s | %s %s %s:%d %.1f", m.Name, data, m.Provenance.Kind, m.Provenance.Rule, m.Provenance.File, m.Provenance.Line, m.Provenance.Confidence))
		}
		lines[n.Identity] = l
	}
	sort.Strings(ids)
	for _, id := range ids {
		out["shapes"] = append(out["shapes"], lines[id]...)
	}
	for _, d := range s.Diagnostics {
		if strings.HasPrefix(d.Code, "route_shape") || d.Code == "openapi_shape_failed" || d.Code == "openapi_coverage" {
			out["diagnostics"] = append(out["diagnostics"], d.File+": "+d.Code+": "+d.Message)
		}
	}
	sort.Strings(out["diagnostics"])
	return out
}

// TestSpecShapeGolden indexes the route-shape-spec fixture (T029,
// docs/contract/claims/route-shape-spec.md): FastAPI routes
// documented by an OpenAPI 3.0, an OpenAPI 3.1 and a Swagger 2.0 spec get
// the committed operations' mapped items beside the code's, with "spec
// differs" notes where the two contradict. A warm run replays every file
// and emits the same records.
func TestSpecShapeGolden(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/route-shape-spec")); err != nil {
		t.Fatal(err)
	}
	reached := map[string]bool{}
	stop := openapi.TraceSpecShapeForms(func(form string) { reached[form] = true })
	s, opt := open(t), options(t)
	data, first, _ := emit(t, root, s, opt)
	stop()
	for _, form := range openapi.SpecShapeForms() {
		if !reached[form] {
			t.Errorf("the fixture never reaches form %s", form)
		}
	}
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/route-shape-spec.golden.json", specShapeSnapshot(t, snapshot))
	again, warm, writes := emit(t, root, s, opt)
	if string(data) != string(again) || warm.Parsed != 0 || warm.Reused != first.Parsed || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0 {
		t.Fatal("warm replay", warm, writes)
	}
}

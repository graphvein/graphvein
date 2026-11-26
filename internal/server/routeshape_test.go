package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/store"
)

// TestRouteShapeServed stores routes with a valid shape, an invalid one
// and none, on both backends, and checks the endpoint list's compact
// shape and the endpoint detail's validated shape (routeshape rows SM-1
// and D): no shape stored means no shape field, an invalid one is never
// served as knowledge.
func TestRouteShapeServed(t *testing.T) {
	shape := routeshape.New(4)
	fact := routeshape.Provenance{Kind: routeshape.Fact, Rule: "SH-T1", File: "app.py", Line: 3, Confidence: 1}
	shape.Path = routeshape.ParamSection{
		Section: routeshape.Section{State: routeshape.Complete, Evidence: &fact},
		Items:   []routeshape.Param{{Name: "id", Access: routeshape.Declared, Presence: routeshape.Required, Schema: &routeshape.Schema{Type: routeshape.TypeInteger}, Provenance: fact}},
	}
	shape.Query.Section = routeshape.Section{State: routeshape.Partial, Reason: "depth cap reached (4)"}
	shape.Header = routeshape.ParamSection{
		Section: routeshape.Section{State: routeshape.Partial, Reason: "dependencies may read more headers"},
		Items: []routeshape.Param{{Name: "X-Token", Access: routeshape.ReadByDependency, Presence: routeshape.PresenceUnknown,
			Provenance: routeshape.Provenance{Kind: routeshape.ReadInHandler, Rule: "SH-T2", File: "deps.py", Line: 7, Chain: []routeshape.Step{{File: "app.py", Line: 3, Function: "token"}}, Confidence: 1}}},
	}
	appJSON := "application/json"
	shape.Models = []routeshape.Model{{Name: "Item", Provenance: fact, Schema: routeshape.Schema{Type: routeshape.TypeObject, Additional: routeshape.AdditionalForbidden, Fields: []routeshape.Field{
		{Name: "id", Presence: routeshape.Required, Schema: routeshape.Schema{Type: routeshape.TypeInteger}},
		{Name: "kind", Presence: routeshape.Optional, Schema: routeshape.Schema{Type: routeshape.TypeEnum, Enum: []any{"a", nil}}},
		{Name: "tags", Presence: routeshape.Optional, Schema: routeshape.Schema{Type: routeshape.TypeArray, Items: &routeshape.Schema{Type: routeshape.TypeUnknown, Reason: "generic"}}},
	}}}}
	shape.Responses = routeshape.ResponseSection{
		Section: routeshape.Section{State: routeshape.Partial, Reason: "error statuses not read"},
		Items: []routeshape.Response{
			{Status: "200", ContentType: &appJSON, Schema: &routeshape.Schema{Type: routeshape.TypeRef, Ref: "Item"}, Provenance: fact},
			{Status: "404", Provenance: routeshape.Provenance{Kind: routeshape.Mapped, Rule: "SH-T3", File: "openapi.yaml", Line: 9, Confidence: 0.9}},
		},
	}
	bearer := "bearer"
	shape.Schemes = []routeshape.Scheme{{Name: "HTTPBearer", Type: routeshape.SchemeHTTP, Scheme: &bearer, Provenance: fact}}
	shape.Security = routeshape.SecuritySection{
		Section: routeshape.Section{State: routeshape.Complete, Evidence: &fact},
		Items:   []routeshape.Requirement{{Schemes: []routeshape.SchemeUse{{Name: "HTTPBearer"}}, Provenance: fact}},
	}
	start := routeshape.Step{File: "app.py", Line: 2, Function: "get_item"}
	steps := []routeshape.Step{{File: "app.py", Line: 4, Function: "a"}, {File: "a.py", Line: 2, Function: "b"}, {File: "b.py", Line: 2, Function: "c"}, {File: "c.py", Line: 2, Function: "d"}}
	shape.Follow.Chains = []routeshape.ChainRecord{{Section: routeshape.SectionQuery, Start: start, Steps: steps, Depth: 4, Stop: routeshape.StopDepthCap, At: &routeshape.Step{File: "d.py", Line: 2, Function: "e"}, Detail: "depth cap reached (4)"}}
	attr, err := routeshape.ToAttribute(shape)
	if err != nil {
		t.Fatal(err)
	}
	span := &contract.Span{StartLine: 2, EndLine: 3, EndColumn: 1}
	route := func(name string, attrs map[string]any) contract.Node {
		attrs["framework"], attrs["method"], attrs["pattern"] = "fastapi", "GET", "/"+name+"/{id}"
		attrs["paths"] = []any{map[string]any{"template": "/" + name + "/{id}", "resolution": "literal"}}
		id := "route:app.py#GET%20%2F" + name + "%2F%7Bid%7D"
		return contract.Node{Type: "node", Identity: id, Kind: "route", Level: 4, Name: "GET /" + name + "/{id}", QualifiedName: "GET /" + name + "/{id}", File: "app.py", Span: span, ContentHash: contract.ContentHash([]byte(id)), Attributes: attrs}
	}
	valid := route("valid", map[string]any{routeshape.AttributeKey: attr})
	invalid := route("invalid", map[string]any{routeshape.AttributeKey: map[string]any{"version": 1}})
	none := route("none", map[string]any{})
	backends(t, func(t *testing.T, w, s store.Store) {
		ctx := context.Background()
		header := contract.Header{Type: "header", ContractVersion: contract.Version, Indexer: contract.Indexer{Name: "graphvein", Version: index.Version}, Repository: "fixture", GeneratedAt: "2026-11-20T00:00:00Z"}
		tx, err := w.BeginRun(ctx, header)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if err := tx.UpsertFile(ctx, contract.File{Type: "file", Path: "app.py", Language: "python", Precision: "best_effort", ContentHash: contract.ContentHash([]byte("app"))}); err != nil {
			t.Fatal(err)
		}
		for _, n := range []contract.Node{valid, invalid, none} {
			if err := tx.UpsertNode(ctx, n); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		opt := testOptions(t.TempDir(), s)
		var list endpointsResponse
		decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/api/endpoints?transport=http&scope=all", nil), &list)
		summaries := map[string]any{}
		for _, tr := range list.Transports {
			rows, _ := tr.Endpoints.([]any)
			for _, r := range rows {
				row := r.(map[string]any)
				summaries[row["id"].(string)] = row["shape"]
			}
		}
		if len(summaries) != 3 || summaries[invalid.Identity] != nil || summaries[none.Identity] != nil {
			t.Fatalf("summaries %v", summaries)
		}
		sum := summaries[valid.Identity].(map[string]any)
		if sum["depth_cap"] != float64(1) || sum["depth_limit"] != float64(4) || sum["sections"].(map[string]any)["path"].(map[string]any)["state"] != "complete" {
			t.Errorf("summary %v", sum)
		}
		detail := func(n contract.Node) map[string]any {
			var out map[string]any
			decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/api/endpoint?scope=all&id="+url.QueryEscape(n.Identity), nil), &out)
			return out
		}
		got := detail(valid)
		if !reflect.DeepEqual(got["shape"], any(attr)) || got["shape_error"] != nil {
			t.Errorf("valid shape served as %v (error %v)", got["shape"], got["shape_error"])
		}
		// The served shape is the golden the web UI's parser tests read
		// (web/src/api/routeShape.test.ts), so Go and TypeScript agree.
		served, err := json.MarshalIndent(got["shape"], "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		served = append(served, '\n')
		want, err := os.ReadFile("testdata/route-shape.golden.json")
		if os.Getenv("GRAPHVEIN_WRITE_ROUTE_SHAPE_GOLDEN") == "1" {
			want, err = served, os.WriteFile("testdata/route-shape.golden.json", served, 0o644)
		}
		if err != nil || string(want) != string(served) {
			t.Errorf("served shape differs from testdata/route-shape.golden.json (%v):\n%s", err, served)
		}
		got = detail(invalid)
		if _, ok := got["shape"]; ok || !strings.Contains(got["shape_error"].(string), "invalid route shape") {
			t.Errorf("invalid shape: %v / %v", got["shape"], got["shape_error"])
		}
		got = detail(none)
		if _, ok := got["shape"]; ok {
			t.Error("a route without a shape has a shape field")
		}
		if _, ok := got["shape_error"]; ok {
			t.Error("a route without a shape has a shape_error field")
		}
		// The stored attribute reads back as the shape that was written.
		nodes, err := s.Nodes(ctx, "fixture", store.Query{Kind: "route", Attribute: routeshape.AttributeKey})
		if err != nil || len(nodes) != 2 {
			t.Fatalf("%d routes with a shape attribute, %v", len(nodes), err)
		}
		for _, n := range nodes {
			if n.Identity != valid.Identity {
				continue
			}
			back, err := routeshape.FromAttribute(n.Attributes[routeshape.AttributeKey])
			if err != nil {
				t.Fatal(err)
			}
			again, err := routeshape.ToAttribute(back)
			if err != nil || !reflect.DeepEqual(again, attr) {
				t.Errorf("stored shape changed: %v", err)
			}
		}
	})
}

package server

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/query"
	"github.com/graphvein/graphvein/internal/store"
)

// TestShapeLinks checks shape_links of GET /api/v0/api/endpoint on both
// backends: one link per chip position (the registration, a security
// dependency's declaration, every shape provenance, never a chain step),
// each equal to presenter.link for that position under the same editor,
// and no link at all when editor links are unavailable.
func TestShapeLinks(t *testing.T) {
	shape := routeshape.New(4)
	fact := routeshape.Provenance{Kind: routeshape.Fact, Rule: "SH-T1", File: "app.py", Line: 3, Confidence: 1}
	shape.Path = routeshape.ParamSection{
		Section: routeshape.Section{State: routeshape.Complete, Evidence: &fact},
		Items:   []routeshape.Param{{Name: "id", Access: routeshape.Declared, Presence: routeshape.Required, Schema: &routeshape.Schema{Type: routeshape.TypeInteger}, Provenance: fact}},
	}
	shape.Header = routeshape.ParamSection{
		Section: routeshape.Section{State: routeshape.Partial, Reason: "dependencies may read more headers"},
		Items: []routeshape.Param{{Name: "X-Token", Access: routeshape.ReadByDependency, Presence: routeshape.PresenceUnknown,
			Provenance: routeshape.Provenance{Kind: routeshape.ReadInHandler, Rule: "SH-T2", File: "deps.py", Line: 7, Chain: []routeshape.Step{{File: "chain.py", Line: 11, Function: "token"}}, Confidence: 1}}},
	}
	shape.Responses = routeshape.ResponseSection{
		Section: routeshape.Section{State: routeshape.Partial, Reason: "error statuses not read"},
		Items:   []routeshape.Response{{Status: "404", Provenance: routeshape.Provenance{Kind: routeshape.Mapped, Rule: "SH-T3", File: "openapi.yaml", Line: 9, Confidence: 0.9}}},
	}
	attr, err := routeshape.ToAttribute(shape)
	if err != nil {
		t.Fatal(err)
	}
	id := "route:app.py#GET%20%2Fitems%2F%7Bid%7D"
	deps := []any{map[string]any{"class": "HTTPBearer", "chain": []any{map[string]any{"file": "deps.py", "span": map[string]any{"start_line": 5, "start_column": 1, "end_line": 5, "end_column": 9}}}}}
	route := contract.Node{
		Type: "node", Identity: id, Kind: "route", Level: 4, Name: "GET /items/{id}", QualifiedName: "GET /items/{id}", File: "app.py",
		Span: &contract.Span{StartLine: 2, EndLine: 3, EndColumn: 1}, ContentHash: contract.ContentHash([]byte(id)),
		Attributes: map[string]any{"framework": "fastapi", "method": "GET", "pattern": "/items/{id}", routeshape.AttributeKey: attr, "security_dependencies": deps},
	}
	sources := map[string]string{
		"app.py":       "app = FastAPI()\n@app.get(\"/items/{id}\")\ndef get_item(id: int):\n    return id\n",
		"deps.py":      "from fastapi import Header\n\n\n\nbearer = HTTPBearer()\n\ndef token(x_token: str = Header()):\n    return x_token\n",
		"openapi.yaml": "openapi: 3.1.0\n\n\n\n\n\n\n\n  '404': {}\n",
	}
	want := []string{"app.py:2", "app.py:3", "deps.py:5", "deps.py:7", "openapi.yaml:9"}
	backends(t, func(t *testing.T, w, s store.Store) {
		ctx := context.Background()
		root := t.TempDir()
		hashes := map[string]string{}
		header := contract.Header{Type: "header", ContractVersion: contract.Version, Indexer: contract.Indexer{Name: "graphvein", Version: index.Version}, Repository: "fixture", GeneratedAt: "2026-11-20T00:00:00Z"}
		tx, err := w.BeginRun(ctx, header)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		for path, text := range sources {
			if err := os.WriteFile(filepath.Join(root, path), []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
			hashes[path] = contract.ContentHash([]byte(text))
			if err := tx.UpsertFile(ctx, contract.File{Type: "file", Path: path, Language: "python", Precision: "best_effort", ContentHash: hashes[path]}); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.UpsertNode(ctx, route); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		opt := testOptions(root, s)
		for _, editor := range []string{"", "zed"} {
			path := "/api/v0/api/endpoint?scope=all&id=" + url.QueryEscape(id)
			var choice *editorChoice
			if editor != "" {
				path += "&editor=" + editor
				q := url.Values{"editor": {editor}}
				if choice, err = editorParam(q); err != nil {
					t.Fatal(err)
				}
			}
			var out endpointResponse
			decodeInto(t, get(t, opt, http.MethodGet, path, nil), &out)
			keys := make([]string, 0, len(out.ShapeLinks))
			for k := range out.ShapeLinks {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			if !reflect.DeepEqual(keys, want) {
				t.Errorf("editor %q: shape_links keys %v, want %v", editor, keys, want)
			}
			p := (&apiHandler{opt: opt}).newPresenter(hashes, choice)
			for _, pos := range shapeChipPositions(route, shape) {
				expected := p.link(pos.file, contract.Span{StartLine: pos.line, EndLine: pos.line})
				got := out.ShapeLinks[shapeLinkKey(pos.file, pos.line)]
				if expected == nil || !reflect.DeepEqual(got, expected) {
					t.Errorf("editor %q: %s:%d link %+v, want presenter.link %+v", editor, pos.file, pos.line, got, expected)
				}
			}
		}
		// Without editor links (graphvein.yaml does not load) no link is
		// served, while the shape still is.
		if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte("version: 1\nide:\n  column_unit: bytes\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		var raw map[string]any
		decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/api/endpoint?scope=all&id="+url.QueryEscape(id), nil), &raw)
		if ide := raw["ide"].(map[string]any); ide["available"] != false {
			t.Fatalf("ide %v, want unavailable", ide)
		}
		if _, ok := raw["shape_links"]; ok {
			t.Errorf("shape_links served without editor links: %v", raw["shape_links"])
		}
		if raw["shape"] == nil {
			t.Error("shape missing")
		}
	})
}

// TestShapeLinksNone checks that a route without positions gets no field.
func TestShapeLinksNone(t *testing.T) {
	if got := shapeLinks(&presenter{ide: ideStatus{Available: true}, p: query.NewPresenter(t.TempDir(), config.DefaultIDE())}, contract.Node{}, nil); got != nil {
		t.Errorf("links %v", got)
	}
}

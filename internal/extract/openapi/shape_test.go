package openapi

import (
	"os"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/clients"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/testscope"
)

const specShapeDoc = "../../../docs/contract/claims/route-shape-spec.md"

// TestSpecShapeContractTable keeps specShapeRows and the claim contract
// document 1:1 (ids, titles, forms, order) and checks that every form
// belongs to one row.
func TestSpecShapeContractTable(t *testing.T) {
	data, err := os.ReadFile(specShapeDoc)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "\n## Claim contract\n")
	if start < 0 {
		t.Fatal("no Claim contract section")
	}
	var docs []specShapeRow
	for _, line := range strings.Split(text[start:], "\n") {
		if !strings.HasPrefix(line, "| SH-O") && !strings.HasPrefix(line, "| D |") {
			continue
		}
		cells := strings.Split(line, "|")
		r := specShapeRow{ID: strings.TrimSpace(cells[1]), Title: strings.TrimSpace(cells[2])}
		for _, f := range strings.Split(cells[3], ",") {
			if f = strings.Trim(strings.TrimSpace(f), "`"); f != "" {
				r.Forms = append(r.Forms, f)
			}
		}
		docs = append(docs, r)
	}
	if len(docs) != len(specShapeRows) {
		t.Fatalf("document has %d rows, code %d", len(docs), len(specShapeRows))
	}
	owner := map[string]string{}
	for i, r := range specShapeRows {
		d := docs[i]
		if d.ID != r.ID || d.Title != r.Title || strings.Join(d.Forms, ",") != strings.Join(r.Forms, ",") {
			t.Errorf("row %d: document %s %q %v, code %s %q %v", i, d.ID, d.Title, d.Forms, r.ID, r.Title, r.Forms)
		}
		for _, f := range r.Forms {
			if prev, dup := owner[f]; dup {
				t.Errorf("form %s in %s and %s", f, prev, r.ID)
			}
			owner[f] = r.ID
		}
	}
	if last := specShapeRows[len(specShapeRows)-1]; last.ID != "D" || len(specShapeRows) > 20 {
		t.Error("rows must end with D and stay within SH-O1..SH-O19")
	}
}

// TestSpecShapeWithoutCodeShape maps a linked operation onto a route that
// has no stored shape (SH-O1): every section starts not_read, the read
// ones become spec-only partial sections and the shape is valid. A depth
// limit of 0 maps nothing, and a broken stored shape is never replaced.
func TestSpecShapeWithoutCodeShape(t *testing.T) {
	route := func(id string) contract.Node {
		return contract.Node{Kind: "route", Identity: id, Attributes: map[string]any{
			"method": "POST", "framework": "express", "paths": []map[string]any{{"template": "/things/{id}"}},
		}}
	}
	sources := map[string][]byte{"openapi.yaml": []byte(`openapi: 3.0.3
info: {title: t, version: "1"}
paths:
  /things/{id}:
    post:
      parameters:
        - {name: id, in: path, schema: {type: string}}
      requestBody:
        content:
          application/json:
            schema: {type: object, properties: {a: {type: string}}}
      responses:
        '201': {description: created}
`)}
	for _, depth := range []int{0, 4} {
		routes := []contract.Node{route("route:app.js#POST /things/:id")}
		matcher := clients.NewRouteMatcher(nil, nil, routes)
		_, _, ds := Link(sources, testscope.Rules{}, testscope.Rules{}, LinkOptions{Matcher: matcher, Routes: routes, ShapeDepth: depth})
		for _, d := range ds {
			if d.Code == "openapi_shape_failed" {
				t.Fatal(d.Message)
			}
		}
		value, ok := routes[0].Attributes[routeshape.AttributeKey]
		if depth == 0 {
			if ok {
				t.Fatal("depth 0 stored a shape")
			}
			continue
		}
		s, err := routeshape.FromAttribute(value)
		if err != nil {
			t.Fatal(err)
		}
		if s.Follow.DepthLimit != depth || s.Path.State != routeshape.Partial || s.Path.CodeState() != routeshape.NotRead ||
			len(s.Path.Items) != 1 || s.Path.Items[0].Presence != routeshape.Required || s.Path.Items[0].Provenance.Confidence != 0.9 {
			t.Fatalf("path %+v", s.Path)
		}
		if s.Query.State != routeshape.NotRead || s.Security.State != routeshape.NotRead {
			t.Fatal("a section the spec says nothing about must stay not_read")
		}
		if s.Body.State != routeshape.Partial || !strings.Contains(s.Body.Reason, "optional") || len(s.Responses.Items) != 1 || s.Responses.Items[0].Schema.Type != routeshape.TypeNull {
			t.Fatalf("body %+v responses %+v", s.Body, s.Responses)
		}
	}
	// A stored shape that does not validate is left as it is, with a
	// diagnostic.
	routes := []contract.Node{route("route:app.js#POST /things/:id")}
	routes[0].Attributes[routeshape.AttributeKey] = map[string]any{"version": 2}
	matcher := clients.NewRouteMatcher(nil, nil, routes)
	_, _, ds := Link(sources, testscope.Rules{}, testscope.Rules{}, LinkOptions{Matcher: matcher, Routes: routes, ShapeDepth: 6})
	failed := 0
	for _, d := range ds {
		if d.Code == "openapi_shape_failed" {
			failed++
		}
	}
	if v := routes[0].Attributes[routeshape.AttributeKey].(map[string]any); failed != 1 || v["version"] != 2 {
		t.Fatal("a broken shape was replaced or not diagnosed", ds)
	}
}

// TestSpecShapeReviewForms covers the forms of the independent review
// that the fixture golden cannot show: a pure reference cycle is
// diagnosed once however many operations read it (SH-O13), media types
// compare without parameters and media ranges cover (SH-O18), and enum
// numbers are stored only when float64 keeps their value (SH-O11).
func TestSpecShapeReviewForms(t *testing.T) {
	sources := map[string][]byte{"openapi.yaml": []byte(`openapi: 3.0.3
info: {title: t, version: "1"}
paths:
  /a:
    get:
      responses:
        '200': {description: ok, content: {application/json: {schema: {$ref: '#/components/schemas/A'}}}}
  /b:
    get:
      responses:
        '200': {description: ok, content: {application/json: {schema: {$ref: '#/components/schemas/B'}}}}
components:
  schemas:
    A: {allOf: [{$ref: '#/components/schemas/B'}]}
    B: {$ref: '#/components/schemas/A'}
`)}
	routes := []contract.Node{
		{Kind: "route", Identity: "route:app.js#GET /a", Attributes: map[string]any{"method": "GET", "framework": "express", "paths": []map[string]any{{"template": "/a"}}}},
		{Kind: "route", Identity: "route:app.js#GET /b", Attributes: map[string]any{"method": "GET", "framework": "express", "paths": []map[string]any{{"template": "/b"}}}},
	}
	matcher := clients.NewRouteMatcher(nil, nil, routes)
	_, _, ds := Link(sources, testscope.Rules{}, testscope.Rules{}, LinkOptions{Matcher: matcher, Routes: routes, ShapeDepth: 6})
	cycles := 0
	for _, d := range ds {
		if d.Code == "openapi_ref_unresolved" && strings.Contains(d.Message, "reference cycle") {
			cycles++
		}
	}
	if cycles != 2 {
		t.Errorf("want one cycle diagnostic per entry schema (A, B), got %d: %v", cycles, ds)
	}
	for _, r := range routes {
		s, err := routeshape.FromAttribute(r.Attributes[routeshape.AttributeKey])
		if err != nil {
			t.Fatal(err)
		}
		if len(s.Responses.Items) != 1 || s.Responses.Items[0].Schema.Type != routeshape.TypeUnknown || s.Responses.Items[0].Schema.Reason != "reference cycle" {
			t.Errorf("%s: responses %+v", r.Identity, s.Responses.Items)
		}
	}

	for _, c := range []struct {
		pattern, key string
		want         bool
	}{
		{mediaKey("Application/JSON; charset=utf-8"), "application/json", true},
		{"*/*", "text/plain", true},
		{"application/*", "application/json", true},
		{"application/*", "text/plain", false},
		{"application/json", "application/xml", false},
	} {
		if got := mediaCovers(c.pattern, c.key); got != c.want {
			t.Errorf("mediaCovers(%q, %q) = %v", c.pattern, c.key, got)
		}
	}

	for _, c := range []struct {
		text string
		want bool
	}{{"9007199254740993", false}, {"9007199254740992", true}, {"0.1", true}, {"-3", true}, {"1.00000000000000001", false}, {"1e2", true}} {
		v := &value{kind: 'v', scalarText: c.text}
		lit, ok := literal(v)
		if !ok {
			t.Fatalf("literal %s", c.text)
		}
		if got := exactNumber(v, lit.(float64)); got != c.want {
			t.Errorf("exactNumber(%s) = %v", c.text, got)
		}
	}
}

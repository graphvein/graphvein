package routeshape

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/graphvein/graphvein/internal/contract"
)

// Model-type edges (rows MT-1..MT-12 of
// docs/contract/claims/route-model-types.md): a route whose shape names a
// model declaration the shape extractor resolved (a Pydantic class, a Go
// struct, a Rust serde type, a TypeScript interface or type alias) uses
// that declaration. The extractor lists the uses on the route node under
// ModelUsesKey; LinkModelUses keeps the uses whose declaration node is
// emitted and adds one UsesModelEdge per route and declaration, so change
// impact can reach the route from a changed model.
const (
	// ModelUsesKey is the route node attribute that lists the model uses
	// (row MT-2).
	ModelUsesKey = "model_uses"
	// UsesModelEdge is the edge kind from a route to a model declaration
	// it uses (row MT-1).
	UsesModelEdge = "uses_model"
)

// ModelUse is one use of a model declaration by a route's shape: the
// declaration, the section and response status of the item that uses it,
// and the item's provenance (row MT-2). The kind is the item's, never the
// model's: one declaration used as a fact body and as a returned response
// is two uses.
type ModelUse struct {
	// Model is the declaration node's identity.
	Model string
	// Section is path, query, header, cookie, body or responses.
	Section string
	// Status is the response status of a responses item, else empty.
	Status string
	Kind   Kind
	// Rule, File and Line are the using item's provenance.
	Rule string
	File string
	Line int
	// Confidence is the item's: 1 for every kind read from code, the
	// inferred link's for a mapped item (stored only for mapped uses; it
	// sets the edge's confidence, row MT-11).
	Confidence float64
}

// ModelUses lists the model declarations every typed item of s uses,
// directly or through the fields, elements and referenced models of its
// schema (rows MT-3, MT-8, MT-9). An item of kind read_in_handler has no
// type and uses nothing (MT-12). The list is sorted and holds one entry
// per (declaration, section, status, kind), the first item's provenance
// kept.
func ModelUses(s *Shape) []ModelUse {
	if s == nil {
		return nil
	}
	models := map[string]*Model{}
	for i := range s.Models {
		models[s.Models[i].Name] = &s.Models[i]
	}
	seen := map[[4]string]bool{}
	var out []ModelUse
	add := func(section, status string, schema *Schema, p Provenance) {
		if schema == nil || p.Kind == ReadInHandler || p.Kind == "" {
			return
		}
		var decls []string
		collectDecls(schema, models, map[string]bool{}, &decls)
		for _, d := range decls {
			key := [4]string{d, section, status, string(p.Kind)}
			if d == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, ModelUse{Model: d, Section: section, Status: status, Kind: p.Kind, Rule: p.Rule, File: p.File, Line: p.Line, Confidence: p.Confidence})
		}
	}
	for _, ps := range []struct {
		name string
		sec  *ParamSection
	}{{SectionPath, &s.Path}, {SectionQuery, &s.Query}, {SectionHeader, &s.Header}, {SectionCookie, &s.Cookie}} {
		for _, p := range ps.sec.Items {
			add(ps.name, "", p.Schema, p.Provenance)
		}
	}
	for _, c := range s.Body.Items {
		add(SectionBody, "", c.Schema, c.Provenance)
	}
	for _, r := range s.Responses.Items {
		add(SectionResponses, r.Status, r.Schema, r.Provenance)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		if a.Section != b.Section {
			return sectionOrder(a.Section) < sectionOrder(b.Section)
		}
		if a.Status != b.Status {
			return a.Status < b.Status
		}
		return a.Kind < b.Kind
	})
	return out
}

func sectionOrder(name string) int {
	for i, s := range SectionNames {
		if s == name {
			return i
		}
	}
	return len(SectionNames)
}

// collectDecls appends the declarations of schema and everything it
// contains, in schema order, each once; refs are followed once per model.
func collectDecls(s *Schema, models map[string]*Model, visited map[string]bool, out *[]string) {
	if s == nil {
		return
	}
	for _, d := range s.Decls {
		if !contains(*out, d) {
			*out = append(*out, d)
		}
	}
	for i := range s.Fields {
		collectDecls(&s.Fields[i].Schema, models, visited, out)
	}
	collectDecls(s.Items, models, visited, out)
	if s.Type == TypeRef && !visited[s.Ref] {
		visited[s.Ref] = true
		if m := models[s.Ref]; m != nil {
			collectDecls(&m.Schema, models, visited, out)
		}
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// UsesValue is the attribute value of uses: plain JSON values, the form
// the store hands back.
func UsesValue(uses []ModelUse) []any {
	out := make([]any, 0, len(uses))
	for _, u := range uses {
		m := map[string]any{"model": u.Model, "section": u.Section, "kind": string(u.Kind), "rule": u.Rule, "file": u.File, "line": u.Line}
		if u.Status != "" {
			m["status"] = u.Status
		}
		if u.Kind == Mapped {
			m["confidence"] = u.Confidence
		}
		out = append(out, m)
	}
	return out
}

// usesOf reads the uses an extractor stored on a node. Entries that do
// not decode are dropped (row D).
func usesOf(n *contract.Node) []map[string]any {
	list, _ := n.Attributes[ModelUsesKey].([]any)
	var out []map[string]any
	for _, v := range list {
		if m, ok := v.(map[string]any); ok {
			if id, _ := m["model"].(string); id != "" {
				out = append(out, m)
			}
		}
	}
	return out
}

// declarationKinds are the node kinds a model declaration can have.
var declarationKinds = map[string]bool{
	"class": true, "struct": true, "interface": true, "type": true, "type_alias": true, "enum": true, "union": true,
	// A named zod or Joi schema is a module-level const (row MT-5).
	"variable": true, "constant": true,
}

// DeclarationKind reports whether a node kind can be a model declaration
// (row MT-3): the kinds change impact follows uses_model edges from.
func DeclarationKind(kind string) bool { return declarationKinds[kind] }

// LinkModelUses keeps, on every route node of nodes, only the model uses
// whose declaration is an emitted node of a declaration kind (row MT-3:
// declared maps each emitted identity to its node), names each use by the
// declaration, and returns one uses_model edge per route and declaration
// (rows MT-1, MT-11). A route whose uses all fall away loses the
// attribute.
func LinkModelUses(nodes []contract.Node, declared map[string]*contract.Node) []contract.Edge {
	var edges []contract.Edge
	for i := range nodes {
		n := &nodes[i]
		if n.Kind != "route" || n.Attributes == nil {
			continue
		}
		if _, has := n.Attributes[ModelUsesKey]; !has {
			continue
		}
		type agg struct {
			fact       bool
			confidence float64
			evidence   []contract.Evidence
		}
		byModel := map[string]*agg{}
		var order []string
		kept := []any{}
		for _, m := range usesOf(n) {
			id := m["model"].(string)
			d := declared[id]
			if d == nil || !DeclarationKind(d.Kind) {
				continue
			}
			kind, _ := m["kind"].(string)
			file, _ := m["file"].(string)
			line := intOf(m["line"])
			confidence := 1.0
			if Kind(kind) == Mapped {
				c, ok := m["confidence"].(float64)
				if n, isNumber := m["confidence"].(json.Number); isNumber {
					f, err := n.Float64()
					c, ok = f, err == nil
				}
				if !ok || c <= 0 || c > 1 {
					// A mapped use without a readable confidence is not
					// proven (row D).
					continue
				}
				confidence = c
			}
			entry := map[string]any{}
			for k, v := range m {
				entry[k] = v
			}
			entry["name"] = d.Name
			kept = append(kept, entry)
			a := byModel[id]
			if a == nil {
				a = &agg{}
				byModel[id] = a
				order = append(order, id)
			}
			if Kind(kind) == Mapped {
				if confidence > a.confidence {
					a.confidence = confidence
				}
			} else {
				a.fact = true
			}
			if file != "" && line >= 1 {
				ev := contract.Evidence{File: file, Span: contract.Span{StartLine: line, EndLine: line}}
				dup := false
				for _, e := range a.evidence {
					dup = dup || e == ev
				}
				if !dup {
					a.evidence = append(a.evidence, ev)
				}
			}
		}
		if len(kept) == 0 {
			delete(n.Attributes, ModelUsesKey)
			continue
		}
		n.Attributes[ModelUsesKey] = kept
		for _, id := range order {
			a := byModel[id]
			e := contract.Edge{Type: "edge", Kind: UsesModelEdge, Source: n.Identity, Target: id, Provenance: "fact", Confidence: 1, Evidence: a.evidence}
			if !a.fact {
				// Row MT-10: only mapped items use it; the edge is inferred
				// with the strongest mapped item's confidence.
				e.Provenance, e.Confidence = "inferred", a.confidence
			}
			if e.Evidence == nil {
				e.Evidence = []contract.Evidence{}
			}
			sort.Slice(e.Evidence, func(i, j int) bool {
				x, y := e.Evidence[i], e.Evidence[j]
				if x.File != y.File {
					return x.File < y.File
				}
				return x.Span.StartLine < y.Span.StartLine
			})
			edges = append(edges, e)
		}
	}
	return edges
}

func intOf(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case float64:
		return int(x)
	case int64:
		return int(x)
	case json.Number:
		n, _ := x.Int64()
		return int(n)
	}
	return 0
}

// String names a use for messages and tests.
func (u ModelUse) String() string {
	s := fmt.Sprintf("%s %s", u.Model, u.Section)
	if u.Status != "" {
		s += " " + u.Status
	}
	return s + " " + string(u.Kind) + " " + u.Rule
}

// WithoutDecls returns a deep copy of s without declaration identities,
// for comparing two schemas as the stored shape sees them.
func WithoutDecls(s *Schema) *Schema {
	if s == nil {
		return nil
	}
	c := *s
	c.Decls = nil
	if s.Fields != nil {
		c.Fields = make([]Field, len(s.Fields))
		for i, f := range s.Fields {
			c.Fields[i] = f
			c.Fields[i].Schema = *WithoutDecls(&f.Schema)
		}
	}
	c.Items = WithoutDecls(s.Items)
	return &c
}

// AddDecls adds the declarations of from (its root and every nested
// node) to the root of into, so a schema kept in place of an equal one
// still names the declarations both were read from.
func AddDecls(into, from *Schema) {
	if into == nil || from == nil {
		return
	}
	var all []string
	collectDecls(from, nil, map[string]bool{}, &all)
	for _, d := range all {
		if !contains(into.Decls, d) {
			into.Decls = append(into.Decls, d)
		}
	}
}

// SetModelUses stores the model uses of s on a route's attributes (row
// MT-2), or nothing when s uses no declaration. Extractors call it with
// the shape they stored under AttributeKey.
func SetModelUses(attrs map[string]any, s *Shape) {
	if attrs == nil {
		return
	}
	if uses := ModelUses(s); len(uses) > 0 {
		attrs[ModelUsesKey] = UsesValue(uses)
	}
}

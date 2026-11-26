package routeshape

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

const modelTypeDoc = "../../../docs/contract/claims/route-model-types.md"

// TestModelTypeContract keeps ModelTypeRows and the document 1:1 (ids,
// order, forms), every form in one row, and every kind the model knows
// in rows MT-9, MT-10 or MT-12.
func TestModelTypeContract(t *testing.T) {
	data, err := os.ReadFile(modelTypeDoc)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "\n## Claim contract\n")
	if start < 0 {
		t.Fatal("no Claim contract section")
	}
	section := text[start+1:]
	// Rows MT-20..MT-29 (the schema view) follow in their own section.
	if end := strings.Index(section, "\n## "); end >= 0 {
		section = section[:end]
	}
	var docs []docRow
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "| MT-") && !strings.HasPrefix(line, "| D |") {
			continue
		}
		cells := strings.Split(line, "|")
		r := docRow{id: strings.TrimSpace(cells[1])}
		for _, f := range strings.Split(cells[3], ",") {
			if f = strings.Trim(strings.TrimSpace(f), "`"); f != "" {
				r.forms = append(r.forms, f)
			}
		}
		docs = append(docs, r)
	}
	if len(docs) != len(ModelTypeRows) {
		t.Fatalf("document has %d rows, code %d", len(docs), len(ModelTypeRows))
	}
	owner := map[string]string{}
	for i, r := range ModelTypeRows {
		if docs[i].id != r.ID || !reflect.DeepEqual(docs[i].forms, r.Forms) {
			t.Errorf("row %d: document %s %v, code %s %v", i, docs[i].id, docs[i].forms, r.ID, r.Forms)
		}
		for _, f := range r.Forms {
			if prev, dup := owner[f]; dup {
				t.Errorf("form %s in %s and %s", f, prev, r.ID)
			}
			owner[f] = r.ID
		}
	}
	for _, k := range []Kind{Fact, Intended, Returned, Mapped, ReadInHandler} {
		if owner["kind:"+string(k)] == "" {
			t.Errorf("kind %s has no row", k)
		}
	}
	if ModelTypeRows[len(ModelTypeRows)-1].ID != "D" {
		t.Error("rows must end with D")
	}
}

func prov(kind Kind, rule string, line int) Provenance {
	return Provenance{Kind: kind, Rule: rule, File: "app.go", Line: line, Chain: []Step{}, Confidence: 1}
}

// TestModelUses checks the rows ModelUses applies: nested fields, items
// and refs (MT-8), one entry per section, status and kind (MT-9), no use
// from read_in_handler or untyped items (MT-12), and mapped uses carrying
// their confidence (MT-10).
func TestModelUses(t *testing.T) {
	s := New(DefaultDepth)
	addr := Schema{Type: TypeObject, Additional: AdditionalAllowed, Decls: []string{"struct:m.go#Address"}}
	user := Schema{Type: TypeObject, Additional: AdditionalAllowed, Fields: []Field{{Name: "home", Presence: Required, Schema: addr}}, Decls: []string{"struct:m.go#User"}}
	list := Schema{Type: TypeArray, Items: &Schema{Type: TypeRef, Ref: "Pet"}}
	s.Models = []Model{{Name: "Pet", Schema: Schema{Type: TypeObject, Additional: AdditionalAllowed, Decls: []string{"class:m.py#Pet", "class:m.py#Base"}}, Provenance: prov(Fact, "SH-F22", 3)}}
	s.Body.Items = []Content{{Schema: &user, Provenance: prov(Fact, "SH-G9", 10)}}
	s.Responses.Items = []Response{
		{Status: "200", Schema: &list, Provenance: prov(Returned, "SH-G21", 12)},
		{Status: "201", Schema: &user, Provenance: prov(Intended, "SH-E9", 13)},
		{Status: "202", Schema: &user, Provenance: Provenance{Kind: Mapped, Rule: "SH-O4", File: "spec.yaml", Line: 4, Chain: []Step{}, Confidence: 0.9}},
	}
	s.Query.Items = []Param{
		{Name: "q", Access: ReadByHandler, Presence: PresenceUnknown, Schema: &user, Provenance: prov(ReadInHandler, "SH-G2", 14)},
		{Name: "r", Access: Declared, Presence: Optional, Provenance: prov(Fact, "SH-G3", 15)},
	}
	var got []string
	for _, u := range ModelUses(s) {
		got = append(got, u.String())
	}
	want := []string{
		"class:m.py#Base responses 200 returned SH-G21",
		"class:m.py#Pet responses 200 returned SH-G21",
		"struct:m.go#Address body fact SH-G9",
		"struct:m.go#Address responses 201 intended SH-E9",
		"struct:m.go#Address responses 202 mapped SH-O4",
		"struct:m.go#User body fact SH-G9",
		"struct:m.go#User responses 201 intended SH-E9",
		"struct:m.go#User responses 202 mapped SH-O4",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("uses:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, v := range UsesValue(ModelUses(s)) {
		m := v.(map[string]any)
		_, has := m["confidence"]
		if has != (m["kind"] == "mapped") {
			t.Errorf("confidence stored %t on a %s use", has, m["kind"])
		}
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "m.go#") {
		t.Error("declarations leaked into the stored shape")
	}
}

// TestLinkModelUses checks MT-1, MT-2, MT-3, MT-10 and MT-11: only
// emitted declaration nodes become edge targets, named on the kept uses;
// an edge is a fact when some use is read from code and inferred with the
// strongest mapped confidence when every use is mapped; a route whose
// uses all fall away loses the attribute.
func TestLinkModelUses(t *testing.T) {
	use := func(model, kind string, line int, confidence any) map[string]any {
		m := map[string]any{"model": model, "section": "body", "kind": kind, "rule": "R", "file": "a.go", "line": float64(line)}
		if confidence != nil {
			m["confidence"] = confidence
		}
		return m
	}
	nodes := []contract.Node{
		{Identity: "route:a.go#GET%20%2Fa", Kind: "route", Attributes: map[string]any{ModelUsesKey: []any{
			use("struct:m.go#A", "fact", 3, nil), use("struct:m.go#A", "mapped", 4, 0.7),
			use("class:m.py#Spec", "mapped", 5, 0.7), use("class:m.py#Spec", "mapped", 6, 0.9),
			use("struct:gone.go#G", "fact", 7, nil), use("function:m.go#f", "fact", 8, nil),
			use("class:m.py#Bad", "mapped", 9, nil),
		}}},
		{Identity: "route:a.go#GET%20%2Fb", Kind: "route", Attributes: map[string]any{ModelUsesKey: []any{use("struct:gone.go#G", "fact", 9, nil)}}},
		{Identity: "server_action:a.ts#x", Kind: "server_action", Attributes: map[string]any{}},
	}
	declared := map[string]*contract.Node{
		"struct:m.go#A":   {Identity: "struct:m.go#A", Kind: "struct", Name: "A"},
		"class:m.py#Spec": {Identity: "class:m.py#Spec", Kind: "class", Name: "Spec"},
		"class:m.py#Bad":  {Identity: "class:m.py#Bad", Kind: "class", Name: "Bad"},
		"function:m.go#f": {Identity: "function:m.go#f", Kind: "function", Name: "f"},
	}
	edges := LinkModelUses(nodes, declared)
	if len(edges) != 2 {
		t.Fatalf("edges = %+v, want 2", edges)
	}
	a, spec := edges[0], edges[1]
	if a.Target != "struct:m.go#A" || a.Provenance != "fact" || a.Confidence != 1 || len(a.Evidence) != 2 {
		t.Errorf("A edge = %+v", a)
	}
	if spec.Target != "class:m.py#Spec" || spec.Provenance != "inferred" || spec.Confidence != 0.9 {
		t.Errorf("Spec edge = %+v", spec)
	}
	kept := nodes[0].Attributes[ModelUsesKey].([]any)
	if len(kept) != 4 {
		t.Errorf("kept %d uses, want 4: %v", len(kept), kept)
	}
	for _, k := range kept {
		if k.(map[string]any)["name"] == "" {
			t.Errorf("use without a name: %v", k)
		}
	}
	if _, has := nodes[1].Attributes[ModelUsesKey]; has {
		t.Error("a route without a proven use keeps the attribute")
	}
}

// TestWithoutDecls: comparing schemas ignores declarations, and AddDecls
// keeps both sides' declarations on the kept schema (MT-8).
func TestWithoutDecls(t *testing.T) {
	a := &Schema{Type: TypeObject, Fields: []Field{{Name: "x", Schema: Schema{Type: TypeString, Decls: []string{"d1"}}}}, Decls: []string{"a"}}
	b := &Schema{Type: TypeObject, Fields: []Field{{Name: "x", Schema: Schema{Type: TypeString}}}, Decls: []string{"b"}}
	if !reflect.DeepEqual(WithoutDecls(a), WithoutDecls(b)) {
		t.Error("schemas differing only in declarations compare unequal")
	}
	if len(a.Decls) != 1 || len(a.Fields[0].Schema.Decls) != 1 {
		t.Error("WithoutDecls changed its argument")
	}
	AddDecls(b, a)
	if !reflect.DeepEqual(b.Decls, []string{"b", "a", "d1"}) {
		t.Errorf("AddDecls = %v", b.Decls)
	}
}

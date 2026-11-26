package shapediff

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/extract/routeshape"
)

const contractDoc = "../../docs/contract/claims/shape-differences.md"

type docRow struct {
	id    string
	forms []string
}

// docRows reads the claim table: the row ID and the backticked forms.
func docRows(t *testing.T) []docRow {
	t.Helper()
	data, err := os.ReadFile(contractDoc)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "\n## Claim contract\n")
	if start < 0 {
		t.Fatal("no Claim contract section")
	}
	var rows []docRow
	for _, line := range strings.Split(text[start:], "\n") {
		if !strings.HasPrefix(line, "| SD-") && !strings.HasPrefix(line, "| D |") {
			continue
		}
		cells := strings.Split(line, "|")
		r := docRow{id: strings.TrimSpace(cells[1])}
		for _, f := range strings.Split(cells[3], ",") {
			if f = strings.Trim(strings.TrimSpace(f), "`"); f != "" {
				r.forms = append(r.forms, f)
			}
		}
		rows = append(rows, r)
	}
	return rows
}

// TestContractTable keeps the code table and the document's 1:1, and
// every form the code knows the form of exactly one row.
func TestContractTable(t *testing.T) {
	docs := docRows(t)
	if len(docs) != len(Rows) {
		t.Fatalf("document has %d rows, code %d", len(docs), len(Rows))
	}
	owner := map[string]string{}
	for i, r := range Rows {
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
	if Rows[len(Rows)-1].ID != "D" {
		t.Error("rows must end with D")
	}
	forms := []string{
		"attribute:" + AttributeKey, "version:1",
		FormFetchJSONBody, FormFetchSearchParams, FormHeyAPIBody, FormHeyAPIQuery,
		"route:" + routeshape.SectionBody, "route:" + routeshape.SectionQuery,
		"difference:" + SentNotRead, "difference:" + ReadNotSent,
		"result:no_difference", "link:cross_repository", "field:shape_differences",
	}
	for _, f := range forms {
		if owner[f] == "" {
			t.Errorf("form %s has no row", f)
		}
		delete(owner, f)
	}
	for f, row := range owner {
		t.Errorf("row %s lists %s, which the code does not have", row, f)
	}
	// The rules the extractor writes into a part are the rows of its forms.
	for rule, form := range map[string]string{"SD-2": FormFetchJSONBody, "SD-3": FormFetchSearchParams, "SD-4": FormHeyAPIBody, "SD-5": FormHeyAPIQuery} {
		for _, r := range Rows {
			if r.ID == rule && (len(r.Forms) != 1 || r.Forms[0] != form) {
				t.Errorf("row %s forms %v, want %s", rule, r.Forms, form)
			}
		}
	}
	for dir, text := range Texts {
		for _, banned := range []string{"unused", "broken", "error"} {
			if strings.Contains(text, banned) {
				t.Errorf("%s: %q says %q", dir, text, banned)
			}
		}
	}
}

func fact(line int) routeshape.Provenance {
	return routeshape.Provenance{Kind: routeshape.Fact, Rule: "SH-T1", File: "app.py", Line: line, Confidence: 1}
}

// shapeWithBody is a route whose JSON body is the model Item (title
// required, note optional) with the given section and item kind.
func shapeWithBody(state routeshape.State, reason string, kind routeshape.Kind, additional routeshape.Additional) *routeshape.Shape {
	s := routeshape.New(6)
	appJSON := "application/json"
	ev := fact(2)
	p := fact(3)
	p.Kind = kind
	s.Body.Section = routeshape.Section{State: state, Reason: reason}
	if state == routeshape.Complete {
		s.Body.Evidence = &ev
	}
	s.Body.Items = []routeshape.Content{{ContentType: &appJSON, Schema: &routeshape.Schema{Type: routeshape.TypeRef, Ref: "Item"}, Provenance: p}}
	mp := fact(5)
	mp.Kind = kind
	s.Models = []routeshape.Model{{Name: "Item", Provenance: mp, Schema: routeshape.Schema{Type: routeshape.TypeObject, Additional: additional, Fields: []routeshape.Field{
		{Name: "title", Presence: routeshape.Required, Schema: routeshape.Schema{Type: routeshape.TypeString}},
		{Name: "note", Presence: routeshape.Optional, Schema: routeshape.Schema{Type: routeshape.TypeString}},
	}}}}
	return s
}

func literalBody(complete bool, keys ...string) *Sends {
	p := &Part{Rule: "SD-2", Form: FormLiteral, ContentType: JSON, File: "web.ts", Line: 9, Complete: complete, Fields: []Field{}}
	for _, k := range keys {
		p.Fields = append(p.Fields, Field{Name: k, File: "web.ts", Line: 10})
	}
	return &Sends{Version: Version, Body: p}
}

// TestCompare checks each comparison and each refusal (rows SD-6..SD-10
// and D) on hand-built shapes.
func TestCompare(t *testing.T) {
	complete := routeshape.Complete
	for _, tc := range []struct {
		name  string
		shape *routeshape.Shape
		sends *Sends
		want  []string // "field direction", or nil for no comparison
	}{
		{"agree", shapeWithBody(complete, "", routeshape.Fact, routeshape.AdditionalUnknown), literalBody(true, "title", "note"), []string{}},
		{"sent not read", shapeWithBody(complete, "", routeshape.Fact, routeshape.AdditionalForbidden), literalBody(true, "title", "color"), []string{"color sent_not_read"}},
		{"read not sent", shapeWithBody(complete, "", routeshape.Fact, routeshape.AdditionalUnknown), literalBody(true, "note"), []string{"title read_not_sent"}},
		{"incomplete literal never says not sent", shapeWithBody(complete, "", routeshape.Fact, routeshape.AdditionalUnknown), literalBody(false, "note"), []string{}},
		{"keeps unlisted keys", shapeWithBody(complete, "", routeshape.Fact, routeshape.AdditionalAllowed), literalBody(true, "color"), nil},
		{"partial", shapeWithBody(routeshape.Partial, "others may exist", routeshape.Fact, routeshape.AdditionalUnknown), literalBody(true, "color"), nil},
		{"complete with a caveat", shapeWithBody(complete, "a middleware is not read", routeshape.Fact, routeshape.AdditionalUnknown), literalBody(true, "color"), nil},
		{"mapped", shapeWithBody(complete, "", routeshape.Mapped, routeshape.AdditionalUnknown), literalBody(true, "color"), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.shape.Body.Items[0].Provenance.Kind == routeshape.Mapped {
				tc.shape.Body.Items[0].Provenance.Confidence = 0.9
				tc.shape.Models[0].Provenance.Confidence = 0.9
			}
			got := Compare(tc.shape, tc.sends)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("compared: %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("nothing compared")
			}
			var lines []string
			for _, d := range got.Differences {
				lines = append(lines, d.Field+" "+d.Direction)
				if d.Text != Texts[d.Direction] || d.Call.File == "" || d.Route.File == "" {
					t.Errorf("difference %+v", d)
				}
			}
			if lines == nil {
				lines = []string{}
			}
			if !reflect.DeepEqual(lines, tc.want) {
				t.Errorf("got %v, want %v", lines, tc.want)
			}
		})
	}
	// A field whose presence is unknown (a serde alias accepts another
	// name): no key of that object is reported as not read.
	s := shapeWithBody(complete, "", routeshape.Fact, routeshape.AdditionalForbidden)
	s.Models[0].Schema.Fields[1].Presence = routeshape.PresenceUnknown
	if got := Compare(s, literalBody(true, "title", "noteAlias")); got == nil || len(got.Differences) != 0 {
		t.Errorf("presence unknown: %+v", got)
	}
	// A required field that accepts null: SD-9 only under a rule that
	// proves it (SH-F22 with Pydantic 2 proven).
	for rule, want := range map[string]int{"SH-T1": 0, "SH-F22": 1} {
		s := shapeWithBody(complete, "", routeshape.Fact, routeshape.AdditionalUnknown)
		s.Models[0].Provenance.Rule = rule
		s.Models[0].Schema.Fields[0].Schema.Nullable = true
		if got := Compare(s, literalBody(true, "note")); got == nil || len(got.Differences) != want {
			t.Errorf("nullable required under %s: %+v, want %d differences", rule, got, want)
		}
	}
	// A depth cap reached while reading the body: nothing compared.
	s = shapeWithBody(complete, "", routeshape.Fact, routeshape.AdditionalUnknown)
	s.Follow.Chains = []routeshape.ChainRecord{{Section: routeshape.SectionBody, Stop: routeshape.StopDepthCap, Depth: 6}}
	if got := Compare(s, literalBody(true, "color")); got != nil {
		t.Errorf("depth cap: %+v", got)
	}
}

// TestAttributeChecks checks that the attribute reader refuses what the
// rules forbid (row SD-1).
func TestAttributeChecks(t *testing.T) {
	good := literalBody(true, "a")
	v, err := ToAttribute(good)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FromAttribute(v); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(m map[string]any){
		"version":       func(m map[string]any) { m["version"] = 2 },
		"unknown field": func(m map[string]any) { m["extra"] = true },
		"typed complete": func(m map[string]any) {
			b := m["body"].(map[string]any)
			b["form"], b["complete"] = FormTyped, true
		},
		"no part": func(m map[string]any) { delete(m, "body") },
		"repeated key": func(m map[string]any) {
			b := m["body"].(map[string]any)
			f := b["fields"].([]any)
			b["fields"] = append(f, f[0])
		},
	} {
		v, _ := ToAttribute(good)
		mutate(v)
		if _, err := FromAttribute(v); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

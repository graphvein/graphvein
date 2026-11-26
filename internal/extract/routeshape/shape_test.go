package routeshape

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func fact(rule string, line int) Provenance {
	return Provenance{Kind: Fact, Rule: rule, File: "app/items.py", Line: line, Confidence: 1}
}

func str(s string) *string { return &s }

// example is a valid shape that uses every section and provenance kind.
func example() *Shape {
	s := New(DefaultDepth)
	dep := []Step{{File: "app/deps.py", Line: 10, Function: "get_token"}}
	s.Models = []Model{
		{Name: "Item", Schema: Schema{Type: TypeObject, Additional: AdditionalForbidden, Fields: []Field{
			{Name: "id", Presence: Required, Schema: Schema{Type: TypeInteger}},
			{Name: "tags", Presence: Optional, Schema: Schema{Type: TypeArray, Items: &Schema{Type: TypeString}}},
			{Name: "kind", Presence: Optional, Schema: Schema{Type: TypeEnum, Enum: []any{"a", "b", nil}}},
		}}, Provenance: fact("SH-F7", 3)},
		{Name: "ItemView", Schema: Schema{Type: TypeObject, Additional: AdditionalUnknown, Fields: []Field{
			{Name: "label", Presence: PresenceUnknown, Schema: Schema{Type: TypeString}},
		}}, Provenance: Provenance{Kind: Intended, Rule: "SH-E2", File: "web/types.ts", Line: 4, Confidence: 1}},
	}
	s.Path = ParamSection{Section: Section{State: Complete, Evidence: ptr(fact("SH-F1", 20))}, Items: []Param{
		{Name: "item_id", Access: Declared, Presence: Required, Schema: &Schema{Type: TypeInteger}, Provenance: fact("SH-F1", 20)},
	}}
	s.Query = ParamSection{Section: Section{State: ProvablyNone, Evidence: ptr(fact("SH-F2", 20))}}
	s.Header = ParamSection{Section: Section{State: Partial, Reason: "a dependency call was not resolved"}, Items: []Param{
		{Name: "X-Token", Access: ReadByDependency, Presence: PresenceUnknown, Provenance: Provenance{Kind: ReadInHandler, Rule: "SH-F3", File: "app/deps.py", Line: 12, Chain: dep, Confidence: 1}},
		{Name: "X-Trace", Access: ReadByHandler, Presence: PresenceUnknown, Provenance: Provenance{Kind: ReadInHandler, Rule: "SH-F3", File: "app/items.py", Line: 22, Confidence: 1}},
	}}
	s.Body = BodySection{Section: Section{State: Complete, Evidence: ptr(fact("SH-F4", 20))}, Items: []Content{
		{ContentType: str("application/json"), Schema: &Schema{Type: TypeRef, Ref: "Item"}, Provenance: fact("SH-F4", 20)},
	}}
	s.Responses = ResponseSection{Section: Section{State: Partial, Reason: "error statuses raised in callees not read"}, Items: []Response{
		{Status: "200", ContentType: str("application/json"), Schema: &Schema{Type: TypeRef, Ref: "Item"}, Provenance: fact("SH-F5", 19)},
		{Status: "200", ContentType: str("application/json"), Schema: &Schema{Type: TypeRef, Ref: "ItemView"}, Provenance: Provenance{Kind: Intended, Rule: "SH-E2", File: "web/api.ts", Line: 8, Confidence: 1}},
		{Status: "404", Provenance: Provenance{Kind: Mapped, Rule: "SH-O1", File: "openapi.yaml", Line: 40, Confidence: 0.9}},
		{Status: "4XX", Schema: &Schema{Type: TypeUnknown, Reason: "error class not followed"}, Provenance: fact("SH-F6", 25)},
	}}
	s.Schemes = []Scheme{{Name: "OAuth2PasswordBearer", Type: SchemeOAuth2, TokenURL: str("/login"), Provenance: fact("SH-F9", 2)}}
	s.Security = SecuritySection{Section: Section{State: Complete, Evidence: ptr(fact("SH-F9", 20))}, Items: []Requirement{
		{Schemes: []SchemeUse{{Name: "OAuth2PasswordBearer"}}, Provenance: fact("SH-F9", 20)},
	}}
	s.Follow.Chains = []ChainRecord{
		{Section: SectionHeader, Start: Step{File: "app/items.py", Line: 18, Function: "read_item"}, Steps: dep, Depth: 1, Stop: StopEnded},
		{Section: SectionResponses, Start: Step{File: "app/items.py", Line: 18, Function: "read_item"}, Steps: dep, Depth: 1, Stop: StopUnresolved, At: &Step{File: "app/deps.py", Line: 14, Function: "decode"}, Detail: "two definitions"},
	}
	return s
}

func ptr(p Provenance) *Provenance { return &p }

func TestExampleIsValidAndRoundTrips(t *testing.T) {
	s := example()
	if vs := Validate(s); vs != nil {
		t.Fatalf("example invalid: %v", vs)
	}
	attr, err := ToAttribute(s)
	if err != nil {
		t.Fatal(err)
	}
	// The stored form is plain JSON: what a store hands back.
	data, err := json.Marshal(attr)
	if err != nil {
		t.Fatal(err)
	}
	var stored any
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(any(attr), stored) {
		t.Fatalf("attribute is not plain JSON:\n%v\n%v", attr, stored)
	}
	back, err := FromAttribute(stored)
	if err != nil {
		t.Fatal(err)
	}
	again, err := ToAttribute(back)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(attr, again) {
		t.Fatalf("round trip changed the attribute:\n%v\n%v", attr, again)
	}
	sum := Summarize(back)
	if sum.Sections[SectionQuery].State != ProvablyNone || sum.Sections[SectionResponses].Items != 4 || sum.DepthLimit != DefaultDepth || sum.DepthCap != 0 {
		t.Fatalf("summary %+v", sum)
	}
}

func TestNewIsAllNotRead(t *testing.T) {
	s := New(DefaultDepth)
	if vs := Validate(s); vs != nil {
		t.Fatal(vs)
	}
	attr, err := ToAttribute(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range SectionNames {
		sec := attr[name].(map[string]any)
		if sec["state"] != string(NotRead) || len(sec["items"].([]any)) != 0 {
			t.Errorf("%s: %v", name, sec)
		}
	}
}

func TestFromAttributeRefuses(t *testing.T) {
	attr, err := ToAttribute(New(DefaultDepth))
	if err != nil {
		t.Fatal(err)
	}
	unknown := map[string]any{}
	for k, v := range attr {
		unknown[k] = v
	}
	unknown["extra"] = true
	other := map[string]any{}
	for k, v := range attr {
		other[k] = v
	}
	other["version"] = 2
	for name, value := range map[string]any{"unknown field": unknown, "other version": other, "not an object": []any{}, "invalid": map[string]any{"version": 1}} {
		if _, err := FromAttribute(value); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// TestViolations breaks each invariant once and expects its row.
func TestViolations(t *testing.T) {
	read := func(line int) Provenance {
		return Provenance{Kind: ReadInHandler, Rule: "SH-E1", File: "a.ts", Line: line, Confidence: 1}
	}
	cases := []struct {
		row    string
		mutate func(s *Shape)
	}{
		{"SM-1", func(s *Shape) { s.Version = 2 }},
		{"SM-1", func(s *Shape) { s.Cookie.State = "maybe" }},
		{"SM-2", func(s *Shape) {
			s.Cookie.Items = []Param{{Name: "c", Access: Declared, Presence: Optional, Provenance: fact("R", 1)}}
		}},
		{"SM-3", func(s *Shape) { s.Query.Evidence = nil }},
		{"SM-3", func(s *Shape) { s.Query.Evidence = ptr(read(3)) }},
		{"SM-4", func(s *Shape) { s.Header.Reason = "" }},
		{"SM-5", func(s *Shape) { s.Security.Evidence = nil }},
		{"SM-5", func(s *Shape) {
			s.Responses.State, s.Responses.Reason, s.Responses.Evidence = Complete, "", ptr(fact("R", 1))
		}},
		{"SM-11", func(s *Shape) { s.Path.Items[0].Provenance.Rule = "" }},
		{"SM-11", func(s *Shape) { s.Path.Items[0].Provenance.Line = 0 }},
		{"SM-11", func(s *Shape) { s.Path.Items[0].Provenance.Kind = "guessed" }},
		{"SM-11", func(s *Shape) { s.Path.Items[0].Provenance.Confidence = 0.9 }},
		{"SM-11", func(s *Shape) { s.Responses.Items[2].Provenance.Confidence = 0 }},
		{"SM-11", func(s *Shape) { s.Header.Items[0].Provenance.Chain = []Step{{File: "x"}} }},
		{"SM-12", func(s *Shape) { s.Header.Items[1].Presence = Required }},
		{"SM-12", func(s *Shape) { s.Header.Items[1].Schema = &Schema{Type: TypeString} }},
		{"SM-12", func(s *Shape) { s.Header.Items[1].Provenance.Kind = Intended }},
		{"SM-12", func(s *Shape) { s.Header.Items[1].Access = Declared }},
		{"SM-12", func(s *Shape) {
			s.Body.State, s.Body.Evidence, s.Body.Reason = Partial, nil, "names read"
			s.Body.Items = []Content{{Schema: &Schema{Type: TypeObject, Additional: AdditionalUnknown, Fields: []Field{{Name: "x", Presence: Optional, Schema: Schema{Type: TypeUnknown, Reason: "not read"}}}}, Provenance: read(5)}}
		}},
		{"SM-12", func(s *Shape) {
			s.Body.State, s.Body.Evidence, s.Body.Reason = Partial, nil, "names read"
			s.Body.Items = []Content{{Schema: &Schema{Type: TypeObject, Additional: AdditionalUnknown, Fields: []Field{{Name: "x", Presence: PresenceUnknown, Schema: Schema{Type: TypeString}}}}, Provenance: read(5)}}
		}},
		{"SM-13", func(s *Shape) { s.Header.Items[0].Provenance.Chain = nil }},
		{"SM-13", func(s *Shape) { s.Header.Items[0].Schema = &Schema{Type: TypeString} }},
		{"SM-14", func(s *Shape) {
			s.Query.State, s.Query.Evidence, s.Query.Reason = Partial, nil, "generic"
			s.Query.Items = []Param{{Name: "q", Access: Declared, Presence: Required, Provenance: Provenance{Kind: Intended, Rule: "SH-E2", File: "a.ts", Line: 1, Confidence: 1}}}
		}},
		{"SM-14", func(s *Shape) { s.Models[1].Schema.Fields[0].Presence = Required }},
		{"SM-15", func(s *Shape) { s.Body.Items[0].Schema.Ref = "ItemView" }},
		{"SM-16", func(s *Shape) { s.Path.Items[0].Presence = Optional }},
		{"SM-16", func(s *Shape) { s.Path.Items[0].Name = "" }},
		{"SM-16", func(s *Shape) {
			h := s.Header.Items[1]
			h.Name = "x-trace"
			s.Header.Items = append(s.Header.Items, h)
		}},
		{"SM-16", func(s *Shape) { s.Path.Items[0].Access = "sometimes" }},
		{"SM-17", func(s *Shape) { s.Body.Items[0].ContentType = str("") }},
		{"SM-17", func(s *Shape) { s.Body.Items = append(s.Body.Items, s.Body.Items[0]) }},
		{"SM-18", func(s *Shape) { s.Responses.Items[0].Status = "2xx" }},
		{"SM-18", func(s *Shape) { s.Responses.Items = append(s.Responses.Items, s.Responses.Items[0]) }},
		{"SM-19", func(s *Shape) { s.Security.Items[0].Schemes[0].Name = "Other" }},
		{"SM-19", func(s *Shape) { s.Schemes[0].In = str("header") }},
		{"SM-19", func(s *Shape) { s.Schemes[0].Type = "magic" }},
		{"SM-19", func(s *Shape) {
			s.Security.State, s.Security.Evidence, s.Security.Reason = Partial, nil, "optional auth guessed"
			s.Security.Items = append(s.Security.Items, Requirement{Provenance: read(9)})
		}},
		{"SM-20", func(s *Shape) { s.Models[0].Schema.Fields[0].Schema = Schema{Type: TypeUnknown} }},
		{"SM-20", func(s *Shape) { s.Models[0].Schema.Fields[1].Schema.Items = nil }},
		{"SM-20", func(s *Shape) { s.Models[0].Schema.Additional = "" }},
		{"SM-20", func(s *Shape) { s.Models[0].Schema.Fields[2].Schema.Enum = []any{map[string]any{}} }},
		{"SM-20", func(s *Shape) { s.Body.Items[0].Schema.Ref = "Missing" }},
		{"SM-20", func(s *Shape) { s.Models = append(s.Models, s.Models[0]) }},
		{"SM-20", func(s *Shape) { s.Path.Items[0].Schema = &Schema{Type: TypeString, Items: &Schema{Type: TypeString}} }},
		{"SM-20", func(s *Shape) {
			deep := Schema{Type: TypeString}
			for range MaxSchemaDepth + 2 {
				inner := deep
				deep = Schema{Type: TypeArray, Items: &inner}
			}
			s.Path.Items[0].Schema = &deep
		}},
		{"SM-21", func(s *Shape) { s.Follow.DepthLimit = 11 }},
		{"SM-21", func(s *Shape) { s.Follow.Chains[0].Depth = 2 }},
		{"SM-21", func(s *Shape) {
			s.Follow.DepthLimit = 1
			s.Header.Items[0].Provenance.Chain = append(s.Header.Items[0].Provenance.Chain, Step{File: "b.py", Line: 1, Function: "f"})
		}},
		{"SM-22", func(s *Shape) { s.Follow.Chains[0].At = &Step{File: "a.py", Line: 1, Function: "f"} }},
		{"SM-23", func(s *Shape) { s.Follow.Chains[1].Stop = StopDepthCap }},
		{"SM-27", func(s *Shape) { s.Follow.Chains[1].At = nil }},
		{"SM-29", func(s *Shape) { s.Follow.Chains[0].Stop = "gave_up" }},
		{"SM-29", func(s *Shape) { s.Follow.Chains[0].Section = "everything" }},
	}
	for i, c := range cases {
		s := example()
		c.mutate(s)
		vs := Validate(s)
		found := false
		for _, v := range vs {
			if v.Row == c.row {
				found = true
			}
		}
		if !found {
			t.Errorf("case %d: want a %s violation, got %v", i, c.row, vs)
		}
		if _, err := ToAttribute(s); err == nil || !strings.Contains(err.Error(), c.row) {
			t.Errorf("case %d: ToAttribute accepted or did not name %s: %v", i, c.row, err)
		}
	}
}

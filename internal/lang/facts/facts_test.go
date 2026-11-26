package facts

import (
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

func TestPythonOverloadParameterTrivia(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"(value: str = 'a b', # note\n other = 1)", "(value:str='a b',other=1)"},
		{"(value = \"# a b\")", "(value=\"# a b\")"},
		{"(value = '''a\n b # text''')", "(value='''a\n b # text''')"},
		{`(value = 'a\' b')`, `(value='a\' b')`},
	} {
		if got := CompactPython(tc.input); got != tc.want {
			t.Errorf("compact(%q)=%q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestUnknownNames(t *testing.T) {
	for _, tc := range []struct{ name, static, want string }{
		{"obj.save", "", "save"}, {"crud::create", "", "create"}, {"@receiver:User.Save", "", "Save"},
		{"@self:User.save", "", "save"}, {"", "", ""}, {"factory(). save", "save", "save"},
	} {
		got := Unknown(Call{Name: tc.name, StaticName: tc.static, Source: "function:a.py#caller"})
		if got.Caller == nil || *got.Caller != "function:a.py#caller" {
			t.Fatal(got)
		}
		if tc.want == "" {
			if got.Name != nil {
				t.Fatal(got)
			}
		} else if got.Name == nil || *got.Name != tc.want {
			t.Fatal(got)
		}
	}
	if Unknown(Call{Name: "save", Source: "file:a.py#a.py"}).Caller != nil {
		t.Fatal("file caller")
	}
}

func TestPythonModelsValid(t *testing.T) {
	valid := func() *PythonModels {
		return &PythonModels{Version: PythonModelsVersion, CoreTables: []int{3}, Classes: []PythonModelClass{{
			Name: "Hero", Line: 1, EndLine: 4, MembersCaptured: true,
			Bases:    []PythonModelExpr{{Kind: "name", Name: "SQLModel"}},
			Keywords: []PythonModelKeyword{{Name: "table", Value: PythonModelExpr{Kind: "literal", LitKind: "bool", Literal: "True"}}},
			Members: []PythonModelMember{
				{Kind: "annotated", Name: "id", Line: 2, Annotation: &PythonModelExpr{Kind: "name", Name: "int"}, Value: &PythonModelExpr{Kind: "call", Name: "Field"}},
				{Kind: "class", Name: "Config", Line: 3, Members: []PythonModelMember{{Kind: "assign", Name: "frozen"}}},
				{Kind: "other", Line: 4, Detail: "if_statement"},
			},
		}}, Sites: []PythonModelSite{{Kind: "append_constraint", Line: 5, Receiver: PythonModelExpr{Kind: "name", Name: "table"}}},
			Queries:     []PythonModelQuery{{Span: contract.Span{StartLine: 6, EndLine: 6}, Owner: "file:a.py#a.py", Expression: PythonModelExpr{Kind: "call", Name: "join", Object: &PythonModelExpr{Kind: "call", Name: "select"}}}},
			Annotations: []PythonModelAnnotations{{Scope: 0, Parameters: map[string]PythonModelExpr{"session": {Kind: "name", Name: "Session"}}}}}
	}
	if !valid().Valid() || !(*PythonModels)(nil).Valid() {
		t.Fatal("valid payload rejected")
	}
	for name, damage := range map[string]func(*PythonModels){
		"version":          func(m *PythonModels) { m.Version = 1 },
		"query line":       func(m *PythonModels) { m.Queries[0].Span.StartLine = 0 },
		"query owner":      func(m *PythonModels) { m.Queries[0].Owner = "" },
		"query kind":       func(m *PythonModels) { m.Queries[0].Expression.Kind = "name" },
		"query object":     func(m *PythonModels) { m.Queries[0].Expression.Object.Kind = "" },
		"object on name":   func(m *PythonModels) { m.Sites[0].Receiver.Object = &PythonModelExpr{Kind: "name"} },
		"annotation empty": func(m *PythonModels) { m.Annotations[0].Parameters = nil },
		"annotation order": func(m *PythonModels) {
			m.Annotations = append(m.Annotations, m.Annotations[0])
		},
		"annotation expression": func(m *PythonModels) { m.Annotations[0].Parameters["session"] = PythonModelExpr{} },
		"class name":            func(m *PythonModels) { m.Classes[0].Name = "" },
		"class line":            func(m *PythonModels) { m.Classes[0].Line = 0 },
		"end line":              func(m *PythonModels) { m.Classes[0].EndLine = 0 },
		"members not taken":     func(m *PythonModels) { m.Classes[0].MembersCaptured = false },
		"member kind":           func(m *PythonModels) { m.Classes[0].Members[0].Kind = "field" },
		"member name":           func(m *PythonModels) { m.Classes[0].Members[0].Name = "" },
		"nested members":        func(m *PythonModels) { m.Classes[0].Members[2].Members = []PythonModelMember{{Kind: "other"}} },
		"expression kind":       func(m *PythonModels) { m.Classes[0].Bases[0].Kind = "attribute" },
		"nested expression":     func(m *PythonModels) { m.Classes[0].Members[0].Value.Args = []PythonModelExpr{{Kind: ""}} },
		"literal kind":          func(m *PythonModels) { m.Classes[0].Keywords[0].Value.LitKind = "" },
		"literal kind value":    func(m *PythonModels) { m.Classes[0].Keywords[0].Value.LitKind = "complex" },
		"core line":             func(m *PythonModels) { m.CoreTables[0] = 0 },
		"site kind":             func(m *PythonModels) { m.Sites[0].Kind = "drop" },
		"site line":             func(m *PythonModels) { m.Sites[0].Line = 0 },
		"annotation on assign": func(m *PythonModels) {
			m.Classes[0].Members[1].Members[0].Annotation = &PythonModelExpr{Kind: "name"}
		},
	} {
		m := valid()
		damage(m)
		if m.Valid() {
			t.Errorf("%s: damaged payload accepted", name)
		}
	}
	p := &PythonFacts{Scopes: []PythonScope{{}}, Models: valid()}
	if !p.ModelsValid() || !(*PythonFacts)(nil).ModelsValid() {
		t.Fatal("valid facts rejected")
	}
	p.Models.Sites[0].Scope = 1
	if p.ModelsValid() {
		t.Fatal("site scope out of range accepted")
	}
	p.Models.Sites[0].Scope = 0
	p.Models.Queries[0].Scope = 1
	if p.ModelsValid() {
		t.Fatal("query scope out of range accepted")
	}
	p.Models.Queries[0].Scope = 0
	p.Models.Annotations[0].Scope = 1
	if p.ModelsValid() {
		t.Fatal("annotation scope out of range accepted")
	}
}

func TestRustQueriesValid(t *testing.T) {
	valid := func() *RustFacts {
		return &RustFacts{Scopes: []RustScope{{}}, Queries: []RustQuery{
			{Path: []string{"sqlx", "query"}, Macro: true, Literal: true, SQL: "select 1", SQLSpan: &contract.Span{StartLine: 1}, Span: contract.Span{StartLine: 1}, Owner: "file:a.rs#a.rs"},
			{Path: []string{"query"}, Span: contract.Span{StartLine: 2}, Owner: "file:a.rs#a.rs"},
		}}
	}
	if !valid().QueriesValid() || !(*RustFacts)(nil).QueriesValid() {
		t.Fatal("valid queries rejected")
	}
	for name, damage := range map[string]func(*RustFacts){
		"path":          func(r *RustFacts) { r.Queries[0].Path = nil },
		"segment":       func(r *RustFacts) { r.Queries[0].Path[1] = "" },
		"owner":         func(r *RustFacts) { r.Queries[0].Owner = "" },
		"line":          func(r *RustFacts) { r.Queries[0].Span.StartLine = 0 },
		"scope":         func(r *RustFacts) { r.Queries[0].Scope = 1 },
		"literal span":  func(r *RustFacts) { r.Queries[0].SQLSpan = nil },
		"sql unliteral": func(r *RustFacts) { r.Queries[1].SQL = "select 1" },
	} {
		r := valid()
		damage(r)
		if r.QueriesValid() {
			t.Errorf("%s: damaged queries accepted", name)
		}
	}
}

package angular

import (
	"reflect"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

func TestUnknownMetadataKeys(t *testing.T) {
	for _, value := range []facts.RouteExpr{{Kind: "identifier", Name: "UNKNOWN"}, {Kind: "number", Name: "42"}} {
		l := &linker{files: map[string]facts.Facts{}}
		d := &declaration{attrs: map[string]any{}, metadata: map[string]expression{}}
		for _, key := range []string{"templateUrl", "standalone", "selector", "pure", "name", "exportAs"} {
			d.metadata[key] = expression{Value: value}
			kind := "string"
			if key == "standalone" || key == "pure" {
				kind = "boolean"
			}
			if got, present := l.value(d, key, kind); got != nil || !present {
				t.Errorf("%s: value %v, present %t", key, got, present)
			}
		}
		want := []string{"exportAs", "name", "pure", "selector", "standalone", "templateUrl"}
		if !reflect.DeepEqual(d.attrs["unknown_keys"], want) {
			t.Error("unknown keys must be sorted", d.attrs)
		}
		for _, diagnostic := range l.diagnostics {
			code, severity := "angular_value_unevaluated", "info"
			if value.Kind == "number" {
				code, severity = "angular_value_wrong_type", "warning"
			}
			if diagnostic.Code != code || diagnostic.Severity != severity {
				t.Error("scalar diagnostic changed", diagnostic)
			}
		}
	}
}

func TestExplicitStandaloneSkipsVersion(t *testing.T) {
	l := &linker{files: map[string]facts.Facts{}, versions: map[string]versionInfo{}}
	d := &declaration{role: "directive", active: true, attrs: map[string]any{}, metadata: map[string]expression{
		"standalone": {Value: facts.RouteExpr{Kind: "identifier", Name: "UNKNOWN"}},
	}}
	l.values(d)
	if d.attrs["standalone"] != nil || d.attrs["standalone_source"] != "unknown" || len(l.versions) != 0 {
		t.Error("explicit unknown fell back to version lookup", d.attrs, l.versions)
	}
	if len(l.diagnostics) != 1 || l.diagnostics[0].Code != "angular_value_unevaluated" {
		t.Error("explicit unknown added a version diagnostic", l.diagnostics)
	}
}

func TestExplicitUnknownPipeValues(t *testing.T) {
	l := &linker{files: map[string]facts.Facts{}}
	d := &declaration{role: "pipe", active: true, attrs: map[string]any{}, metadata: map[string]expression{
		"standalone": {Value: facts.RouteExpr{Kind: "false"}},
		"name":       {Value: facts.RouteExpr{Kind: "identifier", Name: "UNKNOWN"}},
		"pure":       {Value: facts.RouteExpr{Kind: "literal", Name: "wrong type"}},
	}}
	l.values(d)
	if d.attrs["pure"] != nil || d.attrs["name"] != nil || !reflect.DeepEqual(d.attrs["unknown_keys"], []string{"name", "pure"}) {
		t.Error("explicit unknown pipe values took defaults", d.attrs)
	}
	known := &declaration{role: "directive", active: true, attrs: map[string]any{}, metadata: map[string]expression{
		"selector":   {Value: facts.RouteExpr{Kind: "literal", Name: "[known]"}},
		"standalone": {Value: facts.RouteExpr{Kind: "false"}},
	}}
	l.values(known)
	if _, present := known.attrs["unknown_keys"]; present {
		t.Error("known values gained unknown_keys", known.attrs)
	}
}

func TestScopeTargetProof(t *testing.T) {
	for _, tc := range []struct {
		name, identity, role string
		attrs                map[string]any
		code, severity       string
	}{
		{"variable alias", "variable:a.ts#Alias", "", nil, "angular_scope_entry_unresolved", "info"},
		{"function", "function:a.ts#factory", "", nil, "angular_scope_entry_unresolved", "info"},
		{"class expression", "variable:a.ts#Expression", "", nil, "angular_scope_entry_unresolved", "info"},
		{"plain class", "class:a.ts#Plain", "", nil, "angular_scope_entry_invalid", "warning"},
		{"conflict", "class:a.ts#Conflict", "", map[string]any{"conflict": true}, "angular_scope_entry_unresolved", "info"},
		{"absent selector", "class:a.ts#Absent", "directive", map[string]any{"standalone": false, "selector": nil}, "angular_scope_entry_invalid", "warning"},
		{"unknown selector", "class:a.ts#Unknown", "directive", map[string]any{"standalone": false, "selector": nil, "unknown_keys": []string{"selector"}}, "angular_scope_entry_unresolved", "info"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := &linker{classes: map[string]*declaration{}}
			if tc.attrs != nil {
				l.classes[tc.identity] = &declaration{role: tc.role, attrs: tc.attrs}
			}
			counts := map[string]any{"entries": 0, "linked": 0, "external": 0, "unresolved": 0, "invalid": 0}
			l.leaf(&declaration{}, "declarations", "a.ts", contract.Span{}, tc.identity, "", counts, false)
			if len(l.edges) != 0 || len(l.diagnostics) != 1 || l.diagnostics[0].Code != tc.code || l.diagnostics[0].Severity != tc.severity {
				t.Error("target proof", l.edges, l.diagnostics, counts)
			}
		})
	}
}

func TestExternalDeclarationIsUnproven(t *testing.T) {
	l := &linker{}
	counts := map[string]any{"entries": 0, "linked": 0, "external": 0, "unresolved": 0, "invalid": 0}
	l.leaf(&declaration{}, "declarations", "a.ts", contract.Span{}, "", "external:.#npm:library", counts, false)
	if len(l.edges) != 0 || len(l.diagnostics) != 1 || l.diagnostics[0].Code != "angular_scope_entry_unresolved" || l.diagnostics[0].Message != " metadata declarations: target is not a class declaration" {
		t.Error("external package did not prove a class declaration", l.diagnostics, counts)
	}
}

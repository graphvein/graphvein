package facts

import "testing"

// TestRustShapeValid checks that a damaged or foreign Axum shape payload
// is a cache miss, because the linker indexes Scopes with its scopes.
func TestRustShapeValid(t *testing.T) {
	valid := func() *RustFacts {
		return &RustFacts{Scopes: []RustScope{{}}, Shape: &RustShape{Version: RustShapeVersion,
			Types: []RustTypeDecl{{Identity: "struct:a.rs#A", Name: "A", Line: 1}},
			Fns:   []RustFnShape{{Identity: "function:a.rs#f", Line: 2, Uses: []RustUse{{Value: "h", Method: "get", Line: 3}}}}}}
	}
	if !valid().ShapeValid() || !(*RustFacts)(nil).ShapeValid() || !(&RustFacts{}).ShapeValid() {
		t.Fatal("valid payloads refused")
	}
	for name, damage := range map[string]func(*RustFacts){
		"version":    func(r *RustFacts) { r.Shape.Version = 0 },
		"type scope": func(r *RustFacts) { r.Shape.Types[0].Scope = 1 },
		"fn line":    func(r *RustFacts) { r.Shape.Fns[0].Line = 0 },
		"use kind":   func(r *RustFacts) { r.Shape.Fns[0].Uses[0].Method = "" },
		"use value":  func(r *RustFacts) { r.Shape.Fns[0].Uses[0].Value = "" },
	} {
		r := valid()
		damage(r)
		if r.ShapeValid() {
			t.Errorf("%s: damaged payload accepted", name)
		}
	}
}

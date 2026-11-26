package golang

import (
	"reflect"
	"testing"
)

// TestGoPackageCallBindings: the calls Link resolves read the same package
// as import edges (Package). In the fixture, bind/a_gen.go
// (`//go:build ignore`, package main) sorts before bind/lib.go (package
// bind) and declares F too; binduse/u.go calls bind.F() and l.F(). Both
// calls bind to bind/lib.go#F, never to the generator, and the generator's
// own call F() stays in its own file (no configuration builds it into the
// package).
func TestGoPackageCallBindings(t *testing.T) {
	run := runStructure(t, copyStructureFixture(t))
	calls := map[string][]string{}
	for _, e := range run.edges {
		if e.Kind == "calls" {
			calls[e.Source] = append(calls[e.Source], e.Target)
		}
	}
	for source, want := range map[string][]string{
		"function:binduse/u.go#U":     {"function:bind/lib.go#F"},
		"function:bind/a_gen.go#main": {"function:bind/a_gen.go#F"},
	} {
		if got := calls[source]; !reflect.DeepEqual(got, want) {
			t.Errorf("calls from %s: %v, want %v", source, got, want)
		}
	}
	for _, e := range run.edges {
		if e.Kind == "calls" && e.Source == "function:binduse/u.go#U" && len(e.Evidence) != 2 {
			t.Errorf("binduse/u.go#U -> %s: %d evidence entries, want 2 (bind.F() and l.F())", e.Target, len(e.Evidence))
		}
	}
	pkg := Package("bind", run.files)
	if pkg.Name != "bind" || !reflect.DeepEqual(pkg.Files, []string{"bind/lib.go"}) || pkg.Refusal != "" {
		t.Errorf("Package(bind) = %+v", pkg)
	}
}

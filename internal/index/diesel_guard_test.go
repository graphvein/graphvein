package index

import (
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models/diesel"
)

// TestDieselGuard checks ADR 0062 Revision 2 (C) at the index call: a
// failure of the Diesel pass outside its own step guards becomes one
// valid diagnostic and no records, never a failed index run.
func TestDieselGuard(t *testing.T) {
	saved := dieselLink
	t.Cleanup(func() { dieselLink = saved })
	dieselLink = func(diesel.Input) ([]contract.Node, []contract.Edge, []contract.Diagnostic) { panic("injected") }
	nodes, edges, diags := safeDiesel(diesel.Input{})
	if len(nodes) != 0 || len(edges) != 0 || len(diags) != 1 {
		t.Fatalf("output %v %v %v, want one diagnostic only", nodes, edges, diags)
	}
	d := diags[0]
	if d.Code != "schema_model_internal_error" || !strings.Contains(d.Message, "injected") {
		t.Errorf("diagnostic %+v", d)
	}
	if err := validateRecord(d); err != nil {
		t.Errorf("diagnostic is not a valid record: %v", err)
	}
}

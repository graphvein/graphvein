package index

import (
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models/gorm"
)

// TestGormGuard checks T018g5 Revision 2 C1 at the index call: a failure of
// the GORM pass outside its per-module and per-table guards becomes one
// valid diagnostic and no GORM records, never a failed index run.
func TestGormGuard(t *testing.T) {
	saved := gormLink
	t.Cleanup(func() { gormLink = saved })
	gormLink = func(gorm.Input) ([]contract.Node, []contract.Edge, []contract.Diagnostic) { panic("injected") }
	nodes, edges, diags := safeGorm(gorm.Input{})
	if len(nodes) != 0 || len(edges) != 0 || len(diags) != 1 {
		t.Fatalf("got %d nodes, %d edges, %d diagnostics; want one diagnostic only", len(nodes), len(edges), len(diags))
	}
	d := diags[0]
	if d.Code != "schema_model_internal_error" || !strings.Contains(d.Message, "injected") {
		t.Errorf("diagnostic %+v", d)
	}
	if err := validateRecord(d); err != nil {
		t.Errorf("diagnostic is not a valid record: %v", err)
	}
}

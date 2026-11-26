package index

import (
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/extract/tableaccess"
)

// TestTableAccessGuard checks R5-B3 at the index call: a failure of the
// link pass outside its per-file guards becomes one valid diagnostic and
// an empty result, never a failed index run.
func TestTableAccessGuard(t *testing.T) {
	saved := tableAccessLink
	t.Cleanup(func() { tableAccessLink = saved })
	tableAccessLink = func(tableaccess.Input) tableaccess.Output { panic("injected") }
	out := safeTableAccess(tableaccess.Input{})
	if len(out.Edges) != 0 || len(out.Attributes) != 0 || len(out.Diagnostics) != 1 {
		t.Fatalf("output %+v, want one diagnostic only", out)
	}
	d := out.Diagnostics[0]
	if d.Code != tableaccess.InternalErrorCode || !strings.Contains(d.Message, "injected") {
		t.Errorf("diagnostic %+v", d)
	}
	if err := validateRecord(d); err != nil {
		t.Errorf("diagnostic is not a valid record: %v", err)
	}
}

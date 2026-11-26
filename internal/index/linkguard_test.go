package index

import (
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

// TestLinkGuarded checks that a model front end's panic becomes one
// schema_model_internal_error diagnostic and no records (Revision 4 C1),
// and that a front end that returns is passed through unchanged.
func TestLinkGuarded(t *testing.T) {
	nodes, edges, diags := linkGuarded("TypeORM", func() ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
		panic("injected")
	})
	if nodes != nil || edges != nil || len(diags) != 1 || diags[0].Code != "schema_model_internal_error" || !strings.Contains(diags[0].Message, "TypeORM model front end could not be linked: injected") {
		t.Errorf("nodes %v edges %v diagnostics %+v", nodes, edges, diags)
	}
	nodes, _, diags = linkGuarded("TypeORM", func() ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
		return []contract.Node{{Identity: "table:x"}}, nil, nil
	})
	if len(nodes) != 1 || len(diags) != 0 {
		t.Errorf("pass-through: nodes %v diagnostics %v", nodes, diags)
	}
}

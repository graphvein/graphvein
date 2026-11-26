package diesel

import (
	"sort"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
)

// rows names this contract's rows for the model kit's shared steps: the
// table form (DI-T1) for the node shapes and DI-J1 for foreign-key
// resolution. The comparison with migration sets has no Diesel row (no
// tag); Diesel's own migrations are not read (DI-U1).
var rows = models.Rows{Emit: "DI-T1", ForeignKeys: "DI-J1"}

// faultHook is a test-only hook called at the start of every isolated
// step, so a test can make one step fail and check that the others run.
var faultHook func(step, subject string)

// Link discovers every Diesel schema file of the repository (row DI-K1,
// and DI-U2 for the schema.rs files no diesel.toml names), claims its
// tables and columns under the claim contract
// (docs/extract/schema-diesel.md) and emits them through the model kit,
// one group per schema file (models/diesel/<file>). A failure in one file
// or table never stops the others: it becomes a diagnostic.
func Link(in Input) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	steps := &models.Steps{Hook: faultHook}
	// Discovery and each group's emit run guarded too (Revision 2, C): an
	// internal failure there becomes a diagnostic, never a failed index
	// run, and the other groups are still emitted.
	var d *Discovery
	var diags []contract.Diagnostic
	steps.Guard("", "discovery", "Diesel discovery", func() { d, diags = Discover(in) })
	var nodes []contract.Node
	var edges []contract.Edge
	for _, f := range discovered(d) {
		var g *group
		steps.Guard(f.Path, "schema file", "Diesel schema file "+f.Path, func() { g = readGroup(in, f, steps) })
		if g == nil || len(g.tables) == 0 {
			continue
		}
		steps.Guard(f.Path, "emit", "Diesel schema file "+f.Path, func() {
			n, e := models.Link(g.tables, models.LinkInput{Group: g.group(), Rows: rows, Tag: in.Tag, Steps: steps, Sources: in.Sources, Migrations: in.Migrations, Edges: in.Edges})
			nodes = append(nodes, n...)
			edges = append(edges, e...)
		})
	}
	diags = append(diags, steps.Diagnostics...)
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Identity < nodes[j].Identity })
	return nodes, edges, diags
}

// discovered is the discovery's files, none when discovery failed.
func discovered(d *Discovery) []*File {
	if d == nil {
		return nil
	}
	return d.Files
}

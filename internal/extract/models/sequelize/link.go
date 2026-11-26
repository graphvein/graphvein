package sequelize

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
)

// rows names this contract's rows for the kit's shared steps: the model
// forms (SQ-M1) for the node shapes, SQ-A3 for foreign-key resolution.
// The migration comparison has no Sequelize row (no tag), as for Prisma
// and Drizzle.
var rows = models.Rows{Emit: "SQ-M1", ForeignKeys: "SQ-A3"}

// faultHook is a test-only hook called at the start of every isolated
// step.
var faultHook func(step, subject string)

// Link discovers every Sequelize package and instance of the repository,
// claims the tables and columns of the models each instance reaches under
// the claim contract (docs/extract/schema-sequelize.md) and emits them
// through the model kit, one group per instance
// (models/sequelize/<instance file>). A failure in one model or table
// never stops the others: it becomes a diagnostic. The indexer calls it
// once, after the Drizzle front end (internal/index/index.go).
func Link(in Input) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	d, pkgs := runDiscovery(in)
	diags := d.diags
	steps := &models.Steps{Hook: faultHook}
	var nodes []contract.Node
	var edges []contract.Edge
	for _, p := range pkgs {
		if p.Refused != "" {
			continue
		}
		for _, g := range p.Instances {
			if !g.Options.Known {
				// Discovery reported it: every option-derived fact, the table
				// names among them, is unknown.
				continue
			}
			if g.Dialect != "" && !supportedDialects[g.Dialect] {
				// Row D (db2, oracle, snowflake), reported by discovery.
				continue
			}
			gr := &group{d: d, g: g, steps: steps, dialect: g.Dialect}
			steps.Guard(g.File, "group", "Sequelize instance "+g.Root, gr.read)
			if len(gr.tables) == 0 {
				continue
			}
			tables := make([]*models.Table, len(gr.tables))
			for i, t := range gr.tables {
				t.partialReasons()
				tables[i] = t.rec
			}
			n, e := models.Link(tables, models.LinkInput{Group: groupOf(g), Rows: rows, Tag: in.Tag, Steps: steps, Sources: in.Sources, Migrations: in.Migrations, Edges: in.Edges})
			nodes = append(nodes, n...)
			edges = append(edges, e...)
		}
	}
	diags = append(diags, steps.Diagnostics...)
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Identity < nodes[j].Identity })
	return nodes, edges, diags
}

// partialReasons gives a partial column or constraint set a table-level
// reason when none was written: a column that is not read
// makes both sets partial, and the table names it.
func (t *table) partialReasons() {
	has := map[string]bool{}
	for _, r := range t.rec.Reasons {
		if r.Fact != nil {
			has[string(*r.Fact)] = true
		}
	}
	var unread []string
	for _, c := range t.rec.Columns {
		if c.Unknown {
			unread = append(unread, c.Name)
		}
	}
	why := "a fact of the table is not read"
	if len(unread) > 0 {
		why = "the columns " + strings.Join(unread, ", ") + " are not read"
		if len(unread) == 1 {
			why = "the column " + unread[0] + " is not read"
		}
	}
	row := t.formRow()
	if t.join != nil {
		row = "SQ-R2"
	}
	if !t.rec.ColumnsComplete && !has["column_set"] {
		t.reason(row, "column_set", t.m.Span.StartLine, "%s, so the column set is partial", why)
	}
	if !t.rec.ConstraintsComplete && !has["constraint_set"] {
		t.reason(row, "constraint_set", t.m.Span.StartLine, "%s, so the constraint set is partial", why)
	}
}

// groupOf is the kit group of an instance: postgres and sqlite facts are
// oracle (the developer decision of 2026-11-13; step SQB5 compares them),
// mysql, mariadb and mssql facts mapped (SQ-V6), and a group whose dialect
// is not known claims only dialect-independent facts, as mapped.
func groupOf(g *Instance) models.Group {
	verification := models.VerificationMapped
	assumptions := []string{"SQ-V1", "SQ-V2", "SQ-V3", "SQ-V4", "SQ-V5"}
	switch g.Dialect {
	case "postgres", "sqlite":
		verification = models.VerificationOracle
	case "mysql", "mariadb", "mssql":
		assumptions = append(assumptions, "SQ-V6")
	}
	return models.Group{Tool: Tool, Root: g.Root, Pins: g.Package.Pins, Assumptions: assumptions, Dialect: g.Dialect, Verification: verification}
}

// groupFailures refuses the models of the group that sync cannot create
// together (row SQ-P1): two models with one table name (the second CREATE
// TABLE IF NOT EXISTS is a no-op, so the table has the first model's
// columns only), and an explicit index name two tables share in one
// schema (index names are per schema on postgres and per database on
// sqlite, so the second CREATE INDEX fails).
func (gr *group) groupFailures() {
	type key struct{ schema, name string }
	schemaOf := func(t *table) string {
		if t.rec.Schema != nil {
			return *t.rec.Schema
		}
		return ""
	}
	byName := map[key][]*table{}
	byIndex := map[key][]*table{}
	for _, t := range gr.tables {
		k := key{schemaOf(t), t.rec.Name}
		byName[k] = append(byName[k], t)
		seen := map[string]bool{}
		for _, ix := range t.rec.Indexes {
			n, _ := ix["name"].(string)
			if n == "" || seen[n] {
				continue
			}
			seen[n] = true
			ik := key{schemaOf(t), n}
			if gr.dialect == "sqlite" {
				ik.schema = ""
			}
			byIndex[ik] = append(byIndex[ik], t)
		}
		// Two indexes of one table with one name: the second fails.
		count := map[string]int{}
		for _, ix := range t.rec.Indexes {
			if n, _ := ix["name"].(string); n != "" {
				count[n]++
				if count[n] == 2 {
					t.failed = true
					gr.failure(failIndexName, t.m.File, t.m.Span.StartLine, "%s: two indexes named %s: the second CREATE INDEX fails", t.modelName, n)
				}
			}
		}
	}
	for k, list := range byName {
		if len(list) < 2 {
			continue
		}
		for _, t := range list {
			t.failed = true
			gr.failure(failSameTable, t.m.File, t.m.Span.StartLine, "%s: %d models of the group have the table name %s", t.modelName, len(list), k.name)
		}
	}
	for k, list := range byIndex {
		if len(list) < 2 {
			continue
		}
		for _, t := range list {
			t.failed = true
			gr.failure(failIndexName, t.m.File, t.m.Span.StartLine, "%s: the index name %s is used by %d tables; the second CREATE INDEX fails", t.modelName, k.name, len(list))
		}
	}
	var kept []*table
	for _, t := range gr.tables {
		if !t.failed {
			kept = append(kept, t)
		}
	}
	gr.tables = kept
	sort.SliceStable(gr.steps.Diagnostics, func(i, j int) bool { return gr.steps.Diagnostics[i].Message < gr.steps.Diagnostics[j].Message })
	_ = fmt.Sprint
}

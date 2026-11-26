package models

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
)

// Rows names the front end's contract rows for the shared steps, so every
// tag and reason the core writes names a row of that tool's table.
type Rows struct {
	// Emit is the row of the node shapes (SQLModel: L3).
	Emit string
	// ForeignKeys is the row of foreign-key resolution (SQLModel: L4).
	ForeignKeys string
	// Overlap is the row of the comparison with migration sets (SQLModel:
	// L5).
	Overlap string
}

// LinkInput carries what the shared steps read besides the tables.
type LinkInput struct {
	Group Group
	Rows  Rows
	// Tag reports a contract row to the front end's contract test; nil
	// outside tests.
	Tag func(row string)
	// Steps collects diagnostics and isolates each table step.
	Steps *Steps
	// Sources are the repository's sources, for content hashes.
	Sources map[string][]byte
	// Migrations and Edges are the migration pass's records (L5).
	Migrations []contract.Node
	Edges      []contract.Edge
}

func (in LinkInput) tag(row string) {
	if in.Tag != nil && row != "" {
		in.Tag(row)
	}
}

// Link resolves the group's foreign keys, compares its tables with the
// migration sets and emits its nodes and edges. Each table's step runs in
// isolation; diagnostics go to in.Steps.
func Link(tables []*Table, in LinkInput) ([]contract.Node, []contract.Edge) {
	for _, t := range tables {
		if t.ByName == nil {
			t.ByName = map[string]*Column{}
			for _, c := range t.Columns {
				t.ByName[c.Name] = c
			}
		}
	}
	resolveForeignKeys(tables, in)
	compareMigrations(tables, in)
	return emit(tables, in)
}

// resolveForeignKeys resolves every foreign key within the group: the
// target must be exactly one table of the group with that name and schema
// (a test-scope table only for a test-scope source), and the column one of
// its known columns.
func resolveForeignKeys(tables []*Table, in LinkInput) {
	byName := map[string][]*Table{}
	for _, t := range tables {
		byName[t.Name] = append(byName[t.Name], t)
	}
	for _, t := range tables {
		in.Steps.Guard(t.File, "foreign keys", "model table "+t.Identity, func() { tableForeignKeys(t, byName, in) })
	}
}

func sameSchema(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func tableForeignKeys(t *Table, byName map[string][]*Table, in LinkInput) {
	for _, c := range t.Columns {
		for _, fk := range c.ForeignKeys {
			in.tag(in.Rows.ForeignKeys)
			var cands []*Table
			for _, cand := range byName[fk.Table] {
				if !cand.SchemaKnown || !sameSchema(fk.Schema, cand.Schema) {
					continue
				}
				if !t.Test && cand.Test {
					continue
				}
				cands = append(cands, cand)
			}
			switch {
			case len(cands) > 1:
				fk.Status = "ambiguous"
			case len(cands) == 1 && knownColumns(cands[0], fk):
				fk.Status = "resolved"
				fk.Target = ColumnIdentity(cands[0], cands[0].ByName[fk.Column])
			default:
				fk.Status = "unresolved"
			}
			if fk.Status != "resolved" {
				in.Steps.Diag(t.File, "info", "schema_unresolved_reference", fmt.Sprintf("%s:%d: foreign key %s.%s -> %s.%s %s", fk.File, fk.Line, t.Name, c.Name, fk.Table, fk.Column, fk.Status))
			}
		}
	}
}

// knownColumns says whether every column a foreign key references is a
// known column of the target table: the paired Column, and for a key that
// states its whole constraint every RefColumns entry.
func knownColumns(target *Table, fk *ForeignKey) bool {
	for _, name := range append([]string{fk.Column}, fk.RefColumns...) {
		if c := target.ByName[name]; c == nil || c.Unknown {
			return false
		}
	}
	return true
}

// compareMigrations never merges a model table into a migration set; a
// table name both define gets schema_model_migration_overlap, with the
// column-name difference when both tables are complete (ADR 0054 PD2).
func compareMigrations(tables []*Table, in LinkInput) {
	cols := map[string][]string{}
	nodes := map[string]contract.Node{}
	for _, n := range in.Migrations {
		nodes[n.Identity] = n
	}
	for _, e := range in.Edges {
		if e.Kind != "contains" {
			continue
		}
		if c, ok := nodes[e.Target]; ok && c.Kind == "column" {
			if s, ok := nodes[e.Source]; ok && s.Kind == "table" {
				cols[e.Source] = append(cols[e.Source], c.Name)
			}
		}
	}
	for _, t := range tables {
		in.Steps.Guard(t.File, "migration comparison", "model table "+t.Identity, func() { compareTable(t, cols, in) })
	}
}

func compareTable(t *Table, cols map[string][]string, in LinkInput) {
	for _, n := range in.Migrations {
		if n.Kind != "table" || n.Name != t.Name {
			continue
		}
		in.tag(in.Rows.Overlap)
		set, _ := n.Attributes["set"].(string)
		msg := fmt.Sprintf("%s:%d: model table %s (class %s) has the name of table %s of migration set %s; the sources are not merged", t.File, t.ModelLine, t.Name, t.Model, n.Identity, set)
		if state, _ := n.Attributes["state"].(string); state == "complete" && t.ColumnsComplete {
			var onlyModel, onlyMigration []string
			m := map[string]bool{}
			for _, c := range cols[n.Identity] {
				m[c] = true
			}
			for _, c := range t.Columns {
				if !m[c.Name] {
					onlyModel = append(onlyModel, c.Name)
				}
				delete(m, c.Name)
			}
			for c := range m {
				onlyMigration = append(onlyMigration, c)
			}
			sort.Strings(onlyModel)
			sort.Strings(onlyMigration)
			msg += fmt.Sprintf("; columns only in the model: [%s]; only in the migration: [%s]", strings.Join(onlyModel, ", "), strings.Join(onlyMigration, ", "))
		}
		in.Steps.Diag(t.File, "info", "schema_model_migration_overlap", msg)
	}
}

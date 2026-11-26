package drizzle

import (
	"fmt"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// Foreign keys: a column's .references(() => t.col, {onDelete, onUpdate})
// (row DR-C2) and a foreignKey({...}) element (row DR-X1), resolved once
// every table of the group is read, because the referenced table may be
// in another file (dokploy: 119 of 135).

// readReference records a .references() modifier (row DR-C2).
func (c *column) readReference(l link) {
	n := len(l.Args)
	why := ""
	spec := &fkSpec{row: "DR-C2", file: c.file, line: l.Line, cols: []*column{c}, onDelete: "no action", onUpdate: "no action", nameKnown: true}
	switch {
	case n < 1 || n > 2:
		why = ".references() with this number of arguments is not read"
	case l.Args[0].Kind != typescript.ModelArrow || len(l.Args[0].Params) > 0 || l.Args[0].Body == nil:
		why = "the reference is not an arrow returning <table>.<column>"
	}
	if why == "" {
		body := *l.Args[0].Body
		if body.Kind != typescript.ModelMember || body.Object == nil || body.Object.Kind != typescript.ModelIdentifier {
			why = "the reference is not an arrow returning <table>.<column>"
		} else {
			spec.targetFile, spec.targetName, spec.targetKeys = c.file, body.Object.Name, []string{body.Name}
		}
	}
	if why == "" && n == 2 {
		cfg, ok := configValues(&l.Args[1])
		if !ok {
			why = "the actions are not an object literal"
		}
		for k, v := range cfg {
			switch k {
			case "onDelete":
				spec.onDelete = action(v)
			case "onUpdate":
				spec.onUpdate = action(v)
			default:
				why = "the actions object has key " + k
			}
		}
	}
	c.tag("DR-C2")
	if why != "" {
		c.refUnknown = why
		return
	}
	c.refs = append(c.refs, spec)
}

// fkTarget is a foreign key's referenced table and columns.
type fkTarget struct {
	table *table
	cols  []*column
}

// target resolves a foreign key: the referenced table must be a table of
// the group and every referenced key one of its known columns. fail is a
// DR-P1 failure (a key that is not a column of a complete table: drizzle-orm
// reads a property of undefined); why makes the key unknown.
func (g *group) target(t *table, spec *fkSpec, byDecl map[*typescript.ModelDeclaration]*table) (fkTarget, string, string) {
	target := t
	if !spec.self {
		target = nil
		if r := g.in.lookup(spec.targetFile, spec.targetName); r.Decl != nil {
			target = byDecl[r.Decl]
		}
	}
	if target == nil {
		return fkTarget{}, "", fmt.Sprintf("%s is not a table the group claims", spec.targetName)
	}
	out := fkTarget{table: target}
	for _, key := range spec.targetKeys {
		var found *column
		for _, c := range target.columns {
			if c.key == key {
				found = c
			}
		}
		switch {
		case found == nil && target.rec.ColumnsComplete:
			return fkTarget{}, fmt.Sprintf("%s has no column %s", target.rec.Name, key), ""
		case found == nil || found.rec.Unknown:
			return fkTarget{}, "", fmt.Sprintf("column %s of %s is not known", key, target.rec.Name)
		}
		out.cols = append(out.cols, found)
	}
	if g.dialect == "postgresql" {
		cols := fmt.Sprintf("%s(%s)", target.rec.Name, strings.Join(casedNames(out.cols), ", "))
		switch target.referenceKind(out.cols) {
		case refNone:
			return fkTarget{}, cols + " is neither the primary key nor unique, so Postgres rejects the foreign key (there is no unique constraint matching given keys for referenced table)", ""
		case refIndex:
			// applyPgSnapshotsDiff (bin.cjs:28011) writes the foreign
			// keys of created tables before their indexes (:29041-29042),
			// so from one generate or push Postgres rejects this key; it
			// exists only when the index came from an earlier migration.
			return fkTarget{}, "", cols + " is unique only through a unique index, which drizzle-kit creates after the foreign keys (bin.cjs:29041-29042): whether the key exists depends on the migration history"
		}
	}
	return out, "", ""
}

// How a foreign key's referenced columns are keyed on postgresql.
const (
	// refKeyed: the primary key or a unique constraint has exactly these
	// columns, or the table's keys are not fully known.
	refKeyed = iota
	// refIndex: only a non-partial unique index without expressions has
	// them.
	refIndex
	// refNone: nothing does; Postgres rejects the key (DR-P1).
	refNone
)

// referenceKind says how cols of t are keyed for a foreign key on
// postgresql (Postgres's transformFkeyCheckAttrs: a primary key, unique
// constraint or unique index, not partial, without expressions, with
// exactly these columns in any order). A table with an unread modifier,
// an unread extra-config element or unknown columns may hold a key this
// pass does not see: refKeyed, never a DR-P1 failure.
func (t *table) referenceKind(cols []*column) int {
	if t.open || t.multiPK || t.rec.PrimaryKeyOpen || !t.rec.ColumnsComplete {
		return refKeyed
	}
	for _, c := range t.columns {
		if c.rec.Unknown || c.unread != "" {
			return refKeyed
		}
	}
	want := map[*column]bool{}
	for _, c := range cols {
		want[c] = true
	}
	same := func(list []*column) bool {
		if len(list) != len(want) {
			return false
		}
		for _, c := range list {
			if !want[c] {
				return false
			}
		}
		return true
	}
	if t.pk != nil && same(t.pk.cols) {
		return refKeyed
	}
	for _, u := range t.uniques {
		if same(u.cols) {
			return refKeyed
		}
	}
	for _, c := range t.columns {
		if (c.primaryKey || c.unique) && same([]*column{c}) {
			return refKeyed
		}
	}
	for _, ix := range t.uniqueIndexes {
		if same(ix) {
			return refIndex
		}
	}
	return refNone
}

// foreignKeys resolves the group's foreign keys: DR-P1 failures first
// (their tables are not claimed), then every key against the tables that
// remain.
func (g *group) foreignKeys() {
	byDecl := map[*typescript.ModelDeclaration]*table{}
	for _, t := range g.tables {
		byDecl[t.decl] = t
	}
	failed := map[*table]bool{}
	for _, t := range g.tables {
		for _, spec := range t.allFKs() {
			if _, fail, _ := g.target(t, spec, byDecl); fail != "" {
				g.failure(spec.file, spec.line, "%s: foreign key target: %s", t.decl.Name, fail)
				failed[t] = true
			}
		}
	}
	if len(failed) > 0 {
		var kept []*table
		for _, t := range g.tables {
			if !failed[t] {
				kept = append(kept, t)
			} else {
				delete(byDecl, t.decl)
			}
		}
		g.tables = kept
	}
	for _, t := range g.tables {
		t := t
		g.steps.Guard(t.file, "foreign keys", "Drizzle table "+t.rec.Identity, func() {
			for _, spec := range t.allFKs() {
				g.addForeignKey(t, spec, byDecl)
			}
		})
	}
}

// allFKs lists the table's foreign keys in source order of their columns,
// then the extra config's.
func (t *table) allFKs() []*fkSpec {
	var out []*fkSpec
	for _, c := range t.columns {
		out = append(out, c.refs...)
	}
	return append(out, t.fks...)
}

// addForeignKey writes one resolved key on each of its local columns, or
// makes their foreign keys unknown.
func (g *group) addForeignKey(t *table, spec *fkSpec, byDecl map[*typescript.ModelDeclaration]*table) {
	tg, _, why := g.target(t, spec, byDecl)
	if why != "" {
		t.rec.ConstraintsComplete = false
		for _, c := range spec.cols {
			c.rec.ForeignKeysUnknown = true
			c.rec.Reasons = append(c.rec.Reasons, models.Reason{Row: spec.row, Fact: models.Fact("foreign_keys"), File: spec.file, Line: spec.line, Reason: "foreign key not read: " + why})
		}
		return
	}
	name, nameWhy := g.foreignKeyName(t, spec, tg)
	var namePtr *string
	if nameWhy == "" && name != "" {
		namePtr = &name
	} else if nameWhy != "" {
		t.reason(spec.row, "constraint_set", spec.line, "foreign key name: %s", nameWhy)
	}
	var schema *string
	if tg.table.rec.Schema != nil {
		s := *tg.table.rec.Schema
		schema = &s
	}
	local, ref := casedNames(spec.cols), casedNames(tg.cols)
	for i, c := range spec.cols {
		c.rec.ForeignKeys = append(c.rec.ForeignKeys, &models.ForeignKey{
			Schema: schema, Table: tg.table.rec.Name, Column: ref[i], OnDelete: spec.onDelete,
			Columns: local, RefColumns: ref, OnUpdate: spec.onUpdate, Name: namePtr,
			File: spec.file, Line: spec.line,
		})
	}
}

// foreignKeyName is the constraint's name as the database keeps it: the
// literal, else `<table>_<cols>_<reftable>_<refcols>_fk` from the builder
// names (pg-core/foreign-keys.js:44-55), with drizzle-kit's casing pass,
// truncated to 63 bytes on postgresql. sqlite keeps no name
// (inline, unnamed): "" without a reason. mysql: a name
// longer than 64 characters is not known (MySQL rejects it).
func (g *group) foreignKeyName(t *table, spec *fkSpec, tg fkTarget) (string, string) {
	if g.dialect == "sqlite" {
		return "", ""
	}
	if !spec.nameKnown {
		return "", "not a string literal"
	}
	name := ""
	if spec.name != nil {
		name = *spec.name
	} else {
		parts := append([]string{t.rec.Name}, builderNames(spec.cols)...)
		parts = append(parts, tg.table.rec.Name)
		parts = append(parts, builderNames(tg.cols)...)
		name = strings.Join(parts, "_") + "_fk"
	}
	name, ok := t.casedConstraintName(name, spec.cols, tg.cols)
	switch {
	case !ok:
		return "", "the name under casing is not computed"
	case g.dialect == "mysql" && len(name) > 64:
		return "", "longer than MySQL's 64-character limit"
	}
	return t.identifierName(name), ""
}

package sequelize

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// This file reads foreign keys (row SQ-A3): an attribute's own
// `references: {model, key}` with its `onDelete`/`onUpdate` keys, and the
// references an association merged into an attribute (rows SQ-R1, SQ-R2,
// assoc.go), as Sequelize 6.37.8's attributeToSQL writes them (postgres
// dialects/postgres/query-generator.js:378-409, sqlite
// dialects/sqlite/query-generator.js:228-240: `REFERENCES <table>
// (<key or "id">)`, then ON DELETE and ON UPDATE upper-cased when set).
// It also applies the cyclic rule: when the group's
// foreign-key graph is cyclic, sync creates every table without foreign
// keys and adds them in a per-model `alter` pass, which skips primary-key
// columns and stops at the first attribute whose changeColumn SQL the
// database rejects.

// action is an onDelete or onUpdate key as written.
type action struct {
	// present says the key is set (not absent or undefined).
	present bool
	// value is the upper-cased action ("" for a falsy value: Sequelize
	// writes no clause).
	value string
	// why says why the action is not read.
	why string
}

// validActions are the referential actions postgres and sqlite accept as
// Sequelize writes them (upper-cased, after ON DELETE / ON UPDATE).
var validActions = map[string]bool{"CASCADE": true, "SET NULL": true, "SET DEFAULT": true, "RESTRICT": true, "NO ACTION": true}

// readAction reads an onDelete or onUpdate value: a string literal.
func readAction(e *typescript.ModelExpr) action {
	if isUndefined(e) {
		return action{}
	}
	if e.Kind == typescript.ModelBoolean && e.Value == "false" || e.Kind == typescript.ModelNull {
		return action{present: true}
	}
	s, ok := literalString(e)
	switch {
	case !ok:
		return action{present: true, why: "the action is not a string literal"}
	case s == "":
		return action{present: true}
	}
	u := strings.ToUpper(s)
	if !validActions[u] {
		return action{present: true, why: fmt.Sprintf("the action %q is not one of CASCADE, SET NULL, SET DEFAULT, RESTRICT, NO ACTION", s)}
	}
	return action{present: true, value: u}
}

// rendered is the action the database keeps: the written one, else NO
// ACTION (the default when Sequelize writes no clause).
func (a action) rendered() string {
	if a.value == "" {
		return "NO ACTION"
	}
	return a.value
}

// refSpec is a references object: the attribute's own (row SQ-A3) or
// one an association wrote (rows SQ-R1, SQ-R2).
type refSpec struct {
	// row is the row whose branch states the foreign key; file and line
	// locate it.
	row  string
	file string
	line int
	// model is the model key as written (nil when an association wrote
	// the object, or when it is absent).
	model *typescript.ModelExpr
	// key is the referenced column ("" when absent: Sequelize writes
	// "id"); keyGiven says it is written.
	key      string
	keyGiven bool
	// table and schema are the referenced table as the database resolves
	// it; schemaFromHolder is set when the reference is a plain table
	// name, which postgres resolves in the referencing table's schema
	// (query-generator.js:383-390).
	table            string
	schema           *string
	schemaFromHolder bool
	resolved         bool
	// why says why the object is not read.
	why string
}

// readReferences reads an attribute's references object.
func readReferences(e *typescript.ModelExpr, file string, line int) *refSpec {
	r := &refSpec{row: "SQ-A3", file: file, line: line}
	if e.Kind != typescript.ModelObject {
		r.why = "references is not an object literal"
		return r
	}
	for i := range e.Entries {
		en := &e.Entries[i]
		if en.Kind != typescript.ModelProperty && en.Kind != typescript.ModelShorthand {
			r.why = "the references object holds an entry that is not read"
			return r
		}
		v := &en.Value
		switch en.Key {
		case "model":
			if !isUndefined(v) {
				r.model = v
			}
		case "key":
			if isUndefined(v) {
				continue
			}
			s, ok := literalString(v)
			switch {
			case !ok:
				r.why = "the references key is not a string literal"
				return r
			case s != "":
				r.key, r.keyGiven = s, true
			}
		default:
			r.why = "the references key " + en.Key + " is not read"
			return r
		}
	}
	if r.model == nil {
		r.why = "the references object has no model"
	}
	return r
}

// column is the referenced column name.
func (r *refSpec) column() string {
	if r.keyGiven {
		return r.key
	}
	return "id"
}

// resolveReferences resolves the model key of every attribute's own
// references object: a string literal names a table (in the referencing
// table's schema on postgres); a reached model class or define binding
// names that model's table (init replaces it by getTableName(),
// lib/model.js:731-733). Anything else leaves the foreign key unknown.
func (gr *group) resolveReferences(t *table) {
	for _, a := range t.attrs {
		r := a.ref
		if r == nil || r.why != "" || r.resolved {
			continue
		}
		if s, ok := literalString(r.model); ok {
			if s == "" {
				r.why = "the references model is an empty string"
				continue
			}
			r.table, r.resolved, r.schemaFromHolder = s, true, true
			continue
		}
		var target *table
		if t.m.Factory == nil {
			if m := gr.d.modelRef(gr.g, t.m.File, r.model); m != nil {
				target = gr.byModel[m]
			}
		}
		if target == nil || target.failed {
			r.why = "the references model is not a string literal or a claimed model of the group"
			continue
		}
		r.table, r.schema, r.resolved = target.rec.Name, target.rec.Schema, true
		a.refText, a.refKnown = target.rec.Name, true
	}
}

// target is the referenced table name and schema for a table of the
// group's dialect.
func (r *refSpec) target(holder *table) (string, *string) {
	if r.schemaFromHolder && holder.gr.dialect == "postgres" {
		return r.table, holder.rec.Schema
	}
	return r.table, r.schema
}

// fkUnknown makes the column's foreign-key list unknown with a reason.
func (c *colFacts) fkUnknown(row, format string, a ...any) {
	if c.fkWhy != "" || c.rec.Unknown {
		return
	}
	c.fkWhy = fmt.Sprintf(format, a...)
	c.rec.ForeignKeysUnknown = true
	c.rec.ForeignKeys = nil
	delete(c.rec.Claims, "foreign_keys")
	c.reason(row, "foreign_keys", "%s", c.fkWhy)
}

// referenceFailures refuses a table whose references name a column the
// referenced table of the group does not have (row SQ-P1): its CREATE
// TABLE (or the alter pass) fails. Only a referenced table whose column
// set is complete and whose columns are all read is checked. It also
// checks that the referenced column is the primary key or
// unique: postgres, mysql, mariadb and mssql reject a foreign key to a
// column that is neither, so a provably non-unique key refuses the table
// there; sqlite accepts the declaration, and a key not proven unique
// leaves the foreign-key list unknown.
func (gr *group) referenceFailures() {
	for _, t := range gr.tables {
		gr.resolveReferences(t)
	}
	for _, t := range gr.tables {
		for _, c := range t.cols {
			r := c.a.ref
			if c.rec.Unknown || r == nil || !r.resolved || r.why != "" {
				continue
			}
			name, schema := r.target(t)
			tg := gr.tableNamed(name, schema)
			if tg == nil {
				continue
			}
			if tg.rec.ColumnsComplete {
				known, found := true, false
				for _, tc := range tg.rec.Columns {
					known = known && !tc.Unknown
					found = found || tc.Name == r.column()
				}
				if known && !found {
					gr.markDead(t, true, "a references key the target lacks")
					gr.failure(failMissingColumn, c.a.file, c.a.line, "%s.%s: references %s (%s), a column the table %s does not have: the database rejects the foreign key", t.modelName, c.a.key, name, r.column(), name)
					break
				}
			}
			unique, refuted := tg.keyUnique(r.column())
			switch {
			case unique:
			case refuted && gr.dialect != "sqlite" && gr.dialect != "":
				gr.markDead(t, true, "a foreign key to a column that is not unique")
				gr.failure(failKeyNotUnique, r.file, r.line, "%s.%s: references %s (%s), which is neither the primary key nor a unique column of %s: %s rejects the foreign key, so the table is not created", t.modelName, c.a.key, name, r.column(), name, gr.dialect)
			default:
				c.fkUnknown(r.row, "the referenced column %s (%s) is not proven to be the primary key or a unique column of %s (a foreign key needs one)", name, r.column(), name)
			}
			if t.failed {
				break
			}
		}
	}
}

// keyUnique says whether a column of the table is proven to be its only
// primary-key column or a unique column (unique), or proven to be neither
// (refuted: the column is read, the primary key is known, and the unique
// fact is claimed false). The cyclic alter pass and the database both need
// a primary key or unique constraint on exactly that column.
func (t *table) keyUnique(col string) (unique, refuted bool) {
	var tc *colFacts
	for _, c := range t.cols {
		if c.rec.Name == col && !c.rec.Unknown {
			tc = c
		}
	}
	if tc == nil {
		return false, false
	}
	if t.rec.PrimaryKeyOpen || t.unknownEntries || t.mutated != "" {
		if v, ok := tc.rec.Attributes["unique"].(bool); ok && v {
			return true, false
		}
		return false, false
	}
	pks := 0
	for _, c := range t.cols {
		if c.pk {
			pks++
		}
	}
	if tc.pk && pks == 1 {
		return true, false
	}
	if _, claimed := tc.rec.Claims["unique"]; !claimed {
		return false, false
	}
	if v, ok := tc.rec.Attributes["unique"].(bool); ok {
		return v, !v
	}
	return false, false
}

// missingTables refuses the tables whose references name a table of the
// group that sync does not create: a refused table, or a
// model a load-time throw stops. The database rejects the foreign key on
// postgres, mysql, mariadb and mssql, so the referencing table is not
// created either (provable when its target provably is not); sqlite
// accepts the declaration, so the foreign-key list is unknown there.
func (gr *group) missingTables() {
	for changed := true; changed; {
		changed = false
		for _, t := range gr.tables {
			if t.failed {
				continue
			}
			for _, c := range t.cols {
				r := c.a.ref
				if c.rec.Unknown || r == nil || !r.resolved || r.why != "" {
					continue
				}
				name, schema := r.target(t)
				if gr.tableNamed(name, schema) != nil {
					continue
				}
				var dead *deadTable
				for i := range gr.dead {
					if gr.dead[i].name == name && sameSchema(gr.dead[i].schema, schema) {
						dead = &gr.dead[i]
						break
					}
				}
				if dead == nil {
					continue
				}
				if gr.dialect == "sqlite" || gr.dialect == "" {
					c.fkUnknown(r.row, "the referenced table %s is not claimed (%s)", name, dead.why)
					continue
				}
				changed = true
				provable := dead.provable
				gr.markDead(t, provable, "references "+name+", which sync does not create")
				if provable {
					gr.failure(failMissingTable, r.file, r.line, "%s.%s: references %s, which sync does not create (%s): %s rejects the foreign key", t.modelName, c.a.key, name, dead.why, gr.dialect)
				} else {
					gr.notClaimed(t.m, "SQ-P1", "%s.%s references %s, which may not exist (%s)", t.modelName, c.a.key, name, dead.why)
				}
				break
			}
		}
	}
	var kept []*table
	for _, t := range gr.tables {
		if !t.failed {
			kept = append(kept, t)
		}
	}
	gr.tables = kept
}

// tableNamed returns the one table of the group with that name and
// schema, or nil.
func (gr *group) tableNamed(name string, schema *string) *table {
	var found *table
	for _, t := range gr.tables {
		if t.failed || t.rec.Name != name || !sameSchema(t.rec.Schema, schema) {
			continue
		}
		if found != nil {
			return nil
		}
		found = t
	}
	return found
}

// Cyclic states of a group's foreign-key graph.
const (
	acyclic = iota
	cyclic
	cyclicUnknown
)

// cycleState is the result of Sequelize's
// getModelsTopoSortedByForeignKey (lib/model-manager.js:30-67) on the
// group: each model is a node named by its table ("<schema>.<table>"
// when it has a schema), each attribute's references.model an edge
// (references to the model's own table do not count). A cycle among the
// edges read is cyclic; without one, a model, attribute or association
// that is not read may still close one (cyclicUnknown).
func (gr *group) cycleState() (int, string) {
	why := ""
	note := func(s string) {
		if why == "" {
			why = s
		}
	}
	if len(gr.g.Incomplete) > 0 {
		note(gr.g.Incomplete[0])
	}
	if gr.allUnknown != "" {
		note(gr.allUnknown)
	}
	if gr.joinUnknown {
		note("a join table of the group is not claimed")
	}
	nodeOf := func(name string, schema *string) string {
		if schema != nil {
			return *schema + "." + name
		}
		return name
	}
	edges := map[string][]string{}
	all := append([]*table{}, gr.tables...)
	for _, m := range gr.g.Models {
		if gr.byModel[m] == nil {
			note(spanLine(m.File, m.Span) + ": a model of the group is not claimed")
		}
	}
	for _, t := range gr.allTables() {
		found := false
		for _, x := range all {
			found = found || x == t
		}
		if !found {
			all = append(all, t)
		}
	}
	for _, t := range all {
		from := nodeOf(t.rec.Name, t.rec.Schema)
		if t.unknownEntries || t.mutated != "" || t.assocUnknown != "" {
			note(t.rec.Identity + ": an attribute that is not read may hold references")
		}
		for _, a := range t.attrs {
			switch {
			case a.unknown != "" && !a.objectRead:
				note(t.rec.Identity + "." + a.key + ": an attribute that is not read may hold references")
				continue
			case a.ref == nil:
				continue
			case !a.ref.resolved || a.ref.why != "":
				note(t.rec.Identity + "." + a.key + ": a references object that is not read")
				continue
			}
			to := a.ref.table
			if !a.ref.schemaFromHolder {
				to = nodeOf(a.ref.table, a.ref.schema)
			}
			if to != from {
				edges[from] = append(edges[from], to)
			}
		}
	}
	if hasCycle(edges) {
		return cyclic, ""
	}
	if why != "" {
		return cyclicUnknown, why
	}
	return acyclic, ""
}

// allTables are the tables read before the build step (byModel, which
// also holds the ones a provable failure refused later: their models are
// still in Sequelize's model manager).
func (gr *group) allTables() []*table {
	var out []*table
	for _, m := range gr.g.Models {
		if t := gr.byModel[m]; t != nil {
			out = append(out, t)
		}
	}
	return out
}

// hasCycle reports a directed cycle (depth-first search, nodes in sorted
// order for determinism).
func hasCycle(edges map[string][]string) bool {
	state := map[string]int{}
	var visit func(n string) bool
	visit = func(n string) bool {
		switch state[n] {
		case 1:
			return true
		case 2:
			return false
		}
		state[n] = 1
		for _, m := range edges[n] {
			if visit(m) {
				return true
			}
		}
		state[n] = 2
		return false
	}
	var nodes []string
	for n := range edges {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	for _, n := range nodes {
		if visit(n) {
			return true
		}
	}
	return false
}

// alterFails says why a column's changeColumn SQL in the cyclic alter
// pass may be rejected (postgres, query-generator.js:209-245): an ARRAY
// of ENUM ("cannot cast type"), a DEFAULT clause followed by UNIQUE or
// REFERENCES (`SET DEFAULT ([^;]+)` takes the rest of the definition), a
// SERIAL column that is not the primary key (`TYPE SERIAL`), or a column
// that is not read. Primary-key columns are skipped by the pass.
func (c *colFacts) alterFails() string {
	if c.rec.Unknown {
		return "the column " + c.rec.Name + " is not read, so whether its changeColumn SQL is valid is unknown"
	}
	if c.pk {
		return ""
	}
	a := c.a
	switch {
	case a.typ.key == "ARRAY" && a.typ.elem != nil && a.typ.elem.key == "ENUM":
		return c.rec.Name + " is an ARRAY of ENUM, whose changeColumn SQL postgres rejects"
	case c.serial:
		return c.rec.Name + " is a SERIAL column that is not the primary key; its changeColumn writes TYPE SERIAL"
	case (c.schemable || c.defUnknown) && (a.unique.kind == uniqueTrue || a.unique.unknown != "" || a.references):
		return c.rec.Name + " has a DEFAULT followed by UNIQUE or REFERENCES in its definition, which changeColumn's SET DEFAULT takes whole"
	}
	return ""
}

// foreignKeys writes every column's foreign-key list once the group's
// tables are built: an attribute without references has none (row
// SQ-A3, the list proven empty), one with a read references object has
// one foreign key; mssql's attributesToSQL then clears
// actions, and the cyclic rule makes the lists the alter pass may not
// write unknown. The step is all-or-nothing for the
// group: when it fails part way, every list it wrote is made unknown, so
// no list survives without the rules that follow it.
func (gr *group) foreignKeys() {
	state, why := gr.cycleState()
	done := false
	gr.steps.Guard(gr.g.File, "foreign keys", "Sequelize instance "+gr.g.Root, func() {
		for _, t := range gr.tables {
			gr.tableForeignKeys(t, state, why)
		}
		done = true
	})
	if done {
		return
	}
	for _, t := range gr.tables {
		for _, c := range t.cols {
			// A list already unknown keeps its reason.
			c.fkUnknown("SQ-A3", "the foreign-key step of the group failed, so no foreign-key list is claimed")
		}
	}
}

// tableForeignKeys writes one table's lists (foreignKeys).
func (gr *group) tableForeignKeys(t *table, state int, why string) {
	d := gr.dialect
	for _, c := range t.cols {
		gr.columnForeignKeys(t, c)
	}
	if d == "mssql" {
		t.mssqlActions()
	}
	if state == acyclic || d == "sqlite" || d == "" {
		return
	}
	cyc := "the group's foreign-key graph is cyclic"
	if state == cyclicUnknown {
		cyc = "the group's foreign-key graph may be cyclic (" + why + ")"
	}
	if d != "postgres" {
		for _, c := range t.cols {
			if !c.a.references {
				continue
			}
			c.fkUnknown("SQ-A3", "%s: sync adds the foreign keys in an alter pass, which is not mapped on %s", cyc, d)
		}
		return
	}
	fail := ""
	for _, c := range t.cols {
		if fail = c.alterFails(); fail != "" {
			break
		}
	}
	for _, c := range t.unknownCols {
		if fail != "" {
			break
		}
		switch {
		case t.removed[c.a.key] && t.mutated == "", c.a.newVirtual:
			// Not a column of the table (removeAttribute; a
			// `new DataTypes.VIRTUAL(...)` value): no changeColumn.
		default:
			fail = "the column " + c.rec.Name + " is not read, so whether its changeColumn SQL is valid is unknown"
		}
	}
	if fail == "" && (t.unknownEntries || t.mutated != "") {
		fail = "an attribute that is not read may make its changeColumn SQL invalid"
	}
	for _, c := range t.cols {
		switch {
		case !c.a.references:
			// The alter pass adds foreign keys only for references.
		case fail != "":
			c.fkUnknown("SQ-A3", "%s: sync adds the foreign keys in a per-model alter pass that stops at the first invalid changeColumn (%s)", cyc, fail)
		case c.pk && len(c.rec.ForeignKeys) > 0:
			c.fkUnknown("SQ-A3", "%s: the alter pass that adds the foreign keys skips primary-key columns", cyc)
		}
	}
}

// mssqlActions applies what Sequelize 6.37.8's mssql generator does to
// the actions: attributesToSQL
// (dialects/mssql/query-generator.js:542-561) walks the table's
// attributes in order and, for each with references, clears onUpdate on
// the first reference to a table and both actions on every later
// reference to the same table; attributeToSQL (:488-494) clears both on
// a reference to the model's own table. A cleared action writes no clause,
// so the server's default NO ACTION applies. The table a reference names
// is its references.model as written (a table name; init replaces a
// model by its unqualified getTableName(): mssql tables have no schema
// here, row SQ-K2). An attribute whose references target is not read
// (a column that is not read, an unread entry of the attributes object, a
// references model that does not resolve) may be an earlier reference to
// the same table, so every later list is unknown.
func (t *table) mssqlActions() {
	byAttr := map[*attr]*colFacts{}
	for _, c := range t.cols {
		byAttr[c.a] = c
	}
	for _, c := range t.unknownCols {
		byAttr[c.a] = c
	}
	blocked := ""
	if t.unknownEntries || t.mutated != "" {
		blocked = "an attribute that is not read may reference the same table earlier"
	}
	seen := map[string]bool{}
	for _, a := range t.attrs {
		c := byAttr[a]
		if c == nil || t.removed[a.key] && t.mutated == "" {
			// A VIRTUAL attribute (no column) or a removed one.
			continue
		}
		if c.rec.Unknown {
			if (a.unknown == "" || a.objectRead) && !a.references {
				continue
			}
			if a.references && a.ref != nil && a.ref.resolved && a.ref.why == "" {
				name, _ := a.ref.target(t)
				seen[name] = true
				continue
			}
			if blocked == "" {
				blocked = "the attribute " + a.key + " is not read and may reference the same table earlier"
			}
			continue
		}
		if !a.references {
			continue
		}
		r := a.ref
		if r == nil || !r.resolved || r.why != "" {
			if blocked == "" {
				blocked = "the references model of " + a.key + " is not read and may name the same table earlier"
			}
			continue
		}
		name, _ := r.target(t)
		first, self := !seen[name], name == t.rec.Name
		seen[name] = true
		if len(c.rec.ForeignKeys) != 1 {
			continue
		}
		if blocked != "" {
			c.fkUnknown(r.row, "mssql: attributesToSQL clears the actions of a later reference to the same table (query-generator.js:542-561), and %s", blocked)
			continue
		}
		fk := c.rec.ForeignKeys[0]
		fk.OnUpdate = "NO ACTION"
		if !first || self {
			fk.OnDelete = "NO ACTION"
		}
	}
}

// columnForeignKeys writes one column's list.
func (gr *group) columnForeignKeys(t *table, c *colFacts) {
	if c.rec.Unknown || c.fkWhy != "" {
		return
	}
	a := c.a
	if gr.dialect == "" {
		c.fkUnknown("SQ-K1", "the dialect is not known (open point O3), so no constraint list is claimed")
		return
	}
	if !a.references {
		c.rec.ForeignKeysUnknown = false
		c.rec.ForeignKeys = nil
		c.rec.Claims["foreign_keys"] = models.ClaimSource{Row: a.row("SQ-A3")}
		gr.tag(a.row("SQ-A3"))
		return
	}
	r := a.ref
	switch {
	case r == nil:
		c.fkUnknown("SQ-A3", "the references value is not read")
		return
	case r.why != "":
		c.fkUnknown(r.row, "%s", r.why)
		return
	case !r.resolved:
		c.fkUnknown(r.row, "the references model is not resolved")
		return
	case a.onDelete.why != "":
		c.fkUnknown(r.row, "onDelete: %s", a.onDelete.why)
		return
	case a.onUpdate.why != "":
		c.fkUnknown(r.row, "onUpdate: %s", a.onUpdate.why)
		return
	}
	name, schema := r.target(t)
	var sc *string
	if schema != nil {
		s := *schema
		sc = &s
	}
	col := r.column()
	c.rec.ForeignKeysUnknown = false
	c.rec.ForeignKeys = []*models.ForeignKey{{
		Schema: sc, Table: name, Column: col, OnDelete: a.onDelete.rendered(),
		Columns: []string{c.rec.Name}, RefColumns: []string{col}, OnUpdate: a.onUpdate.rendered(),
		File: r.file, Line: r.line,
	}}
	src := models.ClaimSource{Row: r.row}
	if r.file != c.rec.File || r.line != c.rec.Line {
		src.File, src.Line = r.file, r.line
	}
	c.rec.Claims["foreign_keys"] = src
	gr.tag(r.row)
	if claimHook != nil {
		claimHook(c.rec, "foreign_keys", r.row, r.row)
	}
}

func sameSchema(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

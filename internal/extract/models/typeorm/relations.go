package typeorm

import (
	"fmt"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// This file reads the relation members of an entity (rows TO-R1 to TO-R4):
// the owning sides' join columns and foreign keys
// (@ManyToOne, @OneToOne with @JoinColumn), the junction tables of
// @ManyToMany with @JoinTable, and the inverse sides, which have no table
// fact (TO-R3). Citations are to typeorm 0.3.31
// (metadata-builder/RelationJoinColumnBuilder.js,
// metadata-builder/JunctionEntityMetadataBuilder.js) with the 1.1.3
// differences marked.

// relation is one relation member as read from its decorators.
type relation struct {
	t          *table
	kind       string
	file, prop string
	line       int
	member     *typescript.ModelClassMember
	call       *typescript.ModelExpr
	// joinColumn and joinTable are the @JoinColumn and @JoinTable
	// decorator expressions (nil when absent).
	joinColumn, joinTable *typescript.ModelExpr
	// joinCols are an owner's join columns once read.
	joinCols []*column
}

// relationOptions are a relation's literal options.
type relationOptions struct {
	onDelete, onUpdate       string
	onDeleteWhy, onUpdateWhy string
	nullable                 *bool
	nullableWhy              string
	createFK                 bool
	createFKWhy              string
	// fkWhy makes the foreign key unknown (deferrable: D).
	fkWhy string
	// why makes the whole relation unknown.
	why string
}

// fkActions are the referential actions a literal onDelete or onUpdate
// may state (written upper case, metadata/ForeignKeyMetadata.js:32-33).
var fkActions = map[string]bool{"CASCADE": true, "SET NULL": true, "NO ACTION": true, "RESTRICT": true, "SET DEFAULT": true}

// readOptions reads a relation options object literal.
func readRelationOptions(e *typescript.ModelExpr, c *claimer) relationOptions {
	o := relationOptions{createFK: true}
	if e == nil {
		return o
	}
	if e.Kind != typescript.ModelObject {
		o.why = "the relation options are not an object literal"
		return o
	}
	action := func(v *typescript.ModelExpr) (string, string) {
		s, ok := stringLiteral(v)
		if !ok {
			return "", "not a string literal"
		}
		s = strings.ToUpper(s)
		if !fkActions[s] {
			return "", "the action " + s + " is not read"
		}
		return s, ""
	}
	for i := range e.Entries {
		en := &e.Entries[i]
		if en.Kind != typescript.ModelProperty && en.Kind != typescript.ModelShorthand {
			o.why = fmt.Sprintf("a relation option at line %d is not read", en.Span.StartLine)
			return o
		}
		switch en.Key {
		case "onDelete":
			o.onDelete, o.onDeleteWhy = action(&en.Value)
		case "onUpdate":
			o.onUpdate, o.onUpdateWhy = action(&en.Value)
		case "nullable":
			if b, ok := boolLiteral(&en.Value); ok {
				o.nullable = &b
			} else {
				o.nullableWhy = "nullable is not a boolean literal"
			}
		case "createForeignKeyConstraints":
			if b, ok := boolLiteral(&en.Value); ok {
				o.createFK = b
			} else {
				o.createFKWhy = "createForeignKeyConstraints is not a boolean literal"
			}
		case "eager", "lazy", "cascade", "persistence":
			// No table fact (TO-C4's list).
		case "orphanedRowAction":
			// No table fact (row D: not read).
			c.tag("D")
		case "deferrable":
			c.tag("D")
			o.fkWhy = "the deferrable option is not read (row D)"
		default:
			c.tag("D")
			o.why = "the relation option " + en.Key + " is not read (row D)"
			return o
		}
	}
	return o
}

// relationArgs splits a relation decorator call into its target, its
// inverse side (the property name, "" when none is given) and its options.
func (r *relation) args() (target *typescript.ModelExpr, inverse string, inverseKnown bool, opts *typescript.ModelExpr, why string) {
	if r.call == nil || len(r.call.Args) == 0 {
		return nil, "", false, nil, "the relation names no target"
	}
	args := r.call.Args
	target = &args[0]
	rest := args[1:]
	inverseKnown = true
	if len(rest) > 0 && rest[0].Kind != typescript.ModelObject {
		inv := &rest[0]
		rest = rest[1:]
		switch {
		case inv.Kind == typescript.ModelArrow && len(inv.Params) == 1 && inv.Params[0] != "" && !inv.Block && inv.Body != nil &&
			inv.Body.Kind == typescript.ModelMember && inv.Body.Object.Kind == typescript.ModelIdentifier && inv.Body.Object.Name == inv.Params[0]:
			inverse = inv.Body.Name
		default:
			if s, ok := stringLiteral(inv); ok && s != "" {
				inverse = s
			} else {
				inverseKnown = false
			}
		}
	}
	switch len(rest) {
	case 0:
	case 1:
		opts = &rest[0]
	default:
		return nil, "", false, nil, "the relation decorator's arguments are not read"
	}
	return target, inverse, inverseKnown, opts, ""
}

// targetTable resolves a relation target `() => T` (or `(type) => T`) to
// the table of a reached entity of the group.
func (c *claimer) targetTable(file string, e *typescript.ModelExpr) (*table, string) {
	if s, ok := stringLiteral(e); ok {
		return nil, "the target is the string " + s + " (not resolved)"
	}
	if e.Kind != typescript.ModelArrow || len(e.Params) > 1 || e.Block || e.Body == nil {
		return nil, "the target is not an arrow returning a class"
	}
	b := c.d.expr(file, e.Body)
	if b.Class == nil {
		return nil, "the target " + memberPath(e.Body) + " does not resolve to a class of the repository"
	}
	t := c.byClass[b.Class]
	if t == nil {
		return nil, "the target " + memberPath(e.Body) + " is not an entity of this group whose table is read"
	}
	return t, ""
}

// joinSpec is one @JoinColumn object (or @JoinTable joinColumn object).
type joinSpec struct {
	name, ref, fkName string
}

// joinSpecs reads @JoinColumn arguments: none, one object literal or an
// array of object literals (TO-R1).
func joinSpecs(dec *typescript.ModelExpr, allowArray bool) ([]joinSpec, string) {
	if dec == nil || dec.Kind != typescript.ModelCall || len(dec.Args) == 0 {
		return nil, ""
	}
	if len(dec.Args) > 1 {
		return nil, "the @JoinColumn arguments are not read"
	}
	var objs []*typescript.ModelExpr
	switch a := &dec.Args[0]; a.Kind {
	case typescript.ModelObject:
		objs = append(objs, a)
	case typescript.ModelArray:
		if !allowArray {
			return nil, "a join column array is not read"
		}
		for i := range a.Elements {
			objs = append(objs, &a.Elements[i])
		}
	default:
		return nil, "the @JoinColumn argument is not an object literal or an array of them"
	}
	var out []joinSpec
	for _, o := range objs {
		s, why := joinObject(o)
		if why != "" {
			return nil, why
		}
		out = append(out, s)
	}
	return out, ""
}

func joinObject(o *typescript.ModelExpr) (joinSpec, string) {
	var s joinSpec
	if o.Kind != typescript.ModelObject {
		return s, "a join column is not an object literal"
	}
	for i := range o.Entries {
		en := &o.Entries[i]
		if en.Kind != typescript.ModelProperty {
			return s, fmt.Sprintf("a join column option at line %d is not read", en.Span.StartLine)
		}
		v, ok := stringLiteral(&en.Value)
		if !ok || v == "" {
			return s, "the join column option " + en.Key + " is not a non-empty string literal"
		}
		switch en.Key {
		case "name":
			s.name = v
		case "referencedColumnName":
			s.ref = v
		case "foreignKeyConstraintName":
			s.fkName = v
		default:
			return s, "the join column option " + en.Key + " is not read (row D)"
		}
	}
	return s, ""
}

// primaryColumns are a table's read primary columns in column order; why
// is set when the primary key is not known.
func (t *table) primaryColumns() ([]*column, string) {
	if t.pkHidden {
		return nil, "a member of " + t.e.Name + " that is not read may be a primary column, so its primary key is not known"
	}
	var pk []*column
	for _, col := range t.cols {
		if col.unknownWhy != "" {
			return nil, "a column of " + t.e.Name + " is not read"
		}
		if col.primary {
			pk = append(pk, col)
		}
	}
	if len(pk) == 0 {
		return nil, t.e.Name + " has no read primary column"
	}
	return pk, ""
}

// referenced returns the columns a relation references (collectReferencedColumns,
// RelationJoinColumnBuilder.js:103-118 / :118-139): the target's primary
// columns unless a join column names one, then the named ones by
// property, in the target's primary-key order under 1.x when there are
// several.
func (c *claimer) referenced(target *table, specs []joinSpec, line string) ([]*column, string) {
	named := false
	for _, s := range specs {
		named = named || s.ref != ""
	}
	if !named {
		return target.primaryColumns()
	}
	var out []*column
	for _, s := range specs {
		if s.ref == "" {
			return nil, "a join column without referencedColumnName beside one with it is not read"
		}
		var found *column
		for _, col := range target.cols {
			if col.prop == s.ref && col.join == nil {
				found = col
			}
		}
		if found == nil || found.unknownWhy != "" {
			return nil, "the referenced column " + s.ref + " is not a read column of " + target.e.Name
		}
		out = append(out, found)
	}
	if c.g.Package.Line == Line1 && len(out) > 1 {
		pk, why := target.primaryColumns()
		if why != "" {
			return nil, why
		}
		pos := map[*column]int{}
		for i, col := range pk {
			pos[col] = i
		}
		sorted := append([]*column(nil), out...)
		for i := 1; i < len(sorted); i++ {
			for j := i; j > 0 && rank(pos, sorted[j]) < rank(pos, sorted[j-1]); j-- {
				sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
			}
		}
		out = sorted
	}
	return out, ""
}

func rank(pos map[*column]int, col *column) int {
	if i, ok := pos[col]; ok {
		return i
	}
	return 1 << 30
}

// joinInfo is a join column's link to the column it references.
type joinInfo struct {
	ref *column
	// rel is the owning relation (nil for a junction column); row its
	// claim row.
	rel *relation
	row string
	// nullable is the join column's nullability (nil: unknown).
	nullable *bool
	nullWhy  string
}

// relations reads every built table's relation members, after every
// table's own columns are read: owners add their join columns (TO-R1,
// TO-R2), many-to-many owners their junction tables (TO-R4), inverse
// sides are counted (TO-R3). It returns the junction tables.
func (c *claimer) relations(built []*table) []*table {
	c.byClass = map[*typescript.ModelStatement]*table{}
	for _, t := range built {
		c.byClass[t.e.Class] = t
	}
	var junctions []*table
	for _, t := range built {
		for _, r := range t.rels {
			r := r
			c.steps.Guard(r.file, "relation", "TypeORM relation "+t.e.Name+"."+r.prop, func() {
				if j := c.relation(r); j != nil {
					junctions = append(junctions, j)
				}
			})
		}
	}
	return junctions
}

// relation reads one relation member; it returns the junction table of a
// many-to-many owner.
func (c *claimer) relation(r *relation) *table {
	t := r.t
	owner := r.kind == "ManyToOne" || r.kind == "OneToOne" && r.joinColumn != nil
	junction := r.kind == "ManyToMany" && r.joinTable != nil
	if !owner && !junction {
		// TO-R3: an inverse side has no table fact.
		c.tag("TO-R3")
		if r.joinTable != nil || r.joinColumn != nil {
			c.tag("D")
			t.consPartial("D", r.file, r.line, "%s: @JoinColumn or @JoinTable on a @%s is not read (row D)", r.prop, r.kind)
		}
		return nil
	}
	targetExpr, inverse, inverseKnown, optsExpr, why := r.args()
	opts := readRelationOptions(optsExpr, c)
	if why == "" {
		why = opts.why
	}
	var target *table
	if why == "" {
		target, why = c.targetTable(r.file, targetExpr)
	}
	if junction {
		c.tag("TO-R4")
		if why != "" {
			c.unread(r.file, r.line, "the junction table of %s.%s is not read: %s", t.e.Name, r.prop, why)
			t.consPartial("TO-R4", r.file, r.line, "%s: the junction table is not read: %s", r.prop, why)
			return nil
		}
		return c.junction(r, target, inverse, inverseKnown, opts)
	}
	row := "TO-R1"
	if r.kind == "OneToOne" {
		row = "TO-R2"
	}
	c.tag(row)
	fail := func(why string) {
		// The join columns may reuse an explicit column, so no TO-P1 (3)
		// is proven on this entity (objectFailures).
		t.relFailed = true
		t.partialNoKey(row, r.file, r.line, "%s: the join columns are not read: %s", r.prop, why)
		t.consPartial(row, r.file, r.line, "%s: the foreign key and the constraints of the join columns are not read: %s", r.prop, why)
	}
	if why != "" {
		fail(why)
		return nil
	}
	specs, why := joinSpecs(r.joinColumn, true)
	if why != "" {
		fail(why)
		return nil
	}
	refs, why := c.referenced(target, specs, r.file)
	if why != "" {
		fail(why)
		return nil
	}
	for _, ref := range refs {
		if ref.isEnum || ref.join != nil || ref.wrapType {
			fail("the referenced column " + ref.name + " is an enum, join or wrapper-typed column (not read)")
			return nil
		}
	}
	// Names (RelationJoinColumnBuilder.js:137 / :159): a join column that
	// names no referenced column applies to every referenced column.
	names := make([]string, len(refs))
	seen := map[string]bool{}
	var fkName string
	for i, ref := range refs {
		for _, s := range specs {
			if (s.ref == "" || s.ref == ref.prop) && s.name != "" {
				names[i] = s.name
				break
			}
		}
		if names[i] == "" {
			names[i] = c.joinColumnName(r.prop, ref.prop)
		}
		if seen[names[i]] {
			fail("two referenced columns get the join column name " + names[i])
			return nil
		}
		seen[names[i]] = true
	}
	if len(specs) > 0 {
		fkName = specs[0].fkName
	}
	c.tag(c.namingRow())
	nullable, nullWhy := opts.nullable, opts.nullableWhy
	if nullable == nil && nullWhy == "" {
		// metadata/RelationMetadata.js:173-174: nullable unless
		// `nullable: false`.
		nullable = boolPtr(true)
	}
	var cols []*column
	for i, ref := range refs {
		// An explicit column of the same database name is
		// reused: its own facts stay, its type is the referenced column's.
		var merged *column
		for _, col := range t.cols {
			switch {
			case col.name != names[i]:
			case col.join != nil:
				// Two relations sharing a join column are not read.
				fail("another relation already has the join column " + names[i])
				return nil
			default:
				merged = col
			}
		}
		if merged != nil {
			if merged.unknownWhy != "" {
				fail("the explicit column " + merged.name + " it reuses is not read")
				return nil
			}
			merged.mergedRef = ref
			merged.mergedRow = row
			cols = append(cols, merged)
			continue
		}
		col := &column{t: t, file: r.file, prop: r.prop, line: r.line, member: r.member, name: names[i], nameRow: row,
			join: &joinInfo{ref: ref, rel: r, row: row, nullable: nullable, nullWhy: nullWhy}}
		t.cols = append(t.cols, col)
		cols = append(cols, col)
	}
	r.joinCols = cols
	// The foreign key (RelationJoinColumnBuilder.js:62-80).
	switch {
	case opts.createFKWhy != "":
		for _, col := range cols {
			col.fkWhy = opts.createFKWhy
		}
		t.consPartial(row, r.file, r.line, "%s: %s; whether a foreign key and its unique constraint exist is not known", r.prop, opts.createFKWhy)
		return nil
	case !opts.createFK:
		for _, col := range cols {
			col.noFK = true
		}
		return nil
	case opts.fkWhy != "":
		for _, col := range cols {
			col.fkWhy = opts.fkWhy
		}
		t.consPartial("D", r.file, r.line, "%s: %s", r.prop, opts.fkWhy)
		return nil
	}
	fk := &foreignKey{rel: r, row: row, cols: cols, refs: refs, target: target, name: fkName, file: r.file, line: r.line}
	fk.onDelete, fk.onDeleteWhy = actionOrDefault(opts.onDelete, opts.onDeleteWhy, "NO ACTION")
	fk.onUpdate, fk.onUpdateWhy = actionOrDefault(opts.onUpdate, opts.onUpdateWhy, "NO ACTION")
	t.fks = append(t.fks, fk)
	// A one-to-one owner's unique constraint unless every join column is
	// primary (RelationJoinColumnBuilder.js:81-95).
	if r.kind == "OneToOne" {
		allPrimary := true
		for _, col := range cols {
			allPrimary = allPrimary && col.join == nil && col.primary
		}
		if !allPrimary {
			t.relUniques = append(t.relUniques, &uniqueSpec{row: "TO-R2", file: r.file, line: r.line, cols: cols, rel: true})
		}
	}
	return nil
}

// actionOrDefault is a literal action, the default when none is written,
// or unknown.
func actionOrDefault(v, why, def string) (any, string) {
	switch {
	case why != "":
		return nil, why
	case v == "":
		return def, ""
	}
	return v, ""
}

// foreignKey is one foreign key of a table (TO-R1, TO-R2, TO-R4).
type foreignKey struct {
	rel                      *relation
	row                      string
	cols, refs               []*column
	target                   *table
	name                     string
	onDelete, onUpdate       any
	onDeleteWhy, onUpdateWhy string
	file                     string
	line                     int
}

// junction builds a many-to-many owner's junction table (TO-R4,
// JunctionEntityMetadataBuilder.js:27-198 / :27-191): its name, one
// column per referenced column of each side (not null, together the
// primary key), two foreign keys and one index per side.
func (c *claimer) junction(r *relation, target *table, inverse string, inverseKnown bool, opts relationOptions) *table {
	t := r.t
	refuse := func(why string) *table {
		c.unread(r.file, r.line, "the junction table of %s.%s is not read: %s", t.e.Name, r.prop, why)
		t.consPartial("TO-R4", r.file, r.line, "%s: the junction table is not read: %s", r.prop, why)
		return nil
	}
	if opts.createFKWhy != "" || opts.fkWhy != "" {
		return refuse(opts.createFKWhy + opts.fkWhy)
	}
	var name string
	var ownSpec, invSpec []joinSpec
	if dec := r.joinTable; dec.Kind == typescript.ModelCall && len(dec.Args) > 0 {
		if len(dec.Args) > 1 || dec.Args[0].Kind != typescript.ModelObject {
			return refuse("the @JoinTable arguments are not one object literal")
		}
		for i := range dec.Args[0].Entries {
			en := &dec.Args[0].Entries[i]
			if en.Kind != typescript.ModelProperty {
				return refuse(fmt.Sprintf("a @JoinTable option at line %d is not read", en.Span.StartLine))
			}
			switch en.Key {
			case "name":
				s, ok := stringLiteral(&en.Value)
				if !ok || s == "" {
					return refuse("the @JoinTable name is not a non-empty string literal (TO-K2)")
				}
				name = s
			case "joinColumn", "inverseJoinColumn":
				s, why := joinObject(&en.Value)
				if why != "" {
					return refuse(why)
				}
				if en.Key == "joinColumn" {
					ownSpec = []joinSpec{s}
				} else {
					invSpec = []joinSpec{s}
				}
			default:
				// joinColumns/inverseJoinColumns arrays, synchronize,
				// schema, database: the junction unknown (TO-R4 refusal).
				return refuse("the @JoinTable option " + en.Key + " is not read")
			}
		}
	}
	ownRefs, why := c.referenced(t, ownSpec, r.file)
	if why != "" {
		return refuse(why)
	}
	invRefs, why := c.referenced(target, invSpec, r.file)
	if why != "" {
		return refuse(why)
	}
	for _, ref := range append(append([]*column(nil), ownRefs...), invRefs...) {
		if ref.isEnum || ref.join != nil || ref.wrapType {
			return refuse("the referenced column " + ref.name + " is an enum, join or wrapper-typed column (not read)")
		}
	}
	prefix := c.g.EntityPrefix
	ownTable := strings.TrimPrefix(t.rec.Name, prefix)
	invTable := strings.TrimPrefix(target.rec.Name, prefix)
	if name == "" {
		name = joinTableName(ownTable, invTable, r.prop)
	}
	name = prefix + name
	c.tag(c.namingRow())
	colName := func(specs []joinSpec, table string, ref *column) string {
		for _, s := range specs {
			if (s.ref == "" || s.ref == ref.prop) && s.name != "" {
				return s.name
			}
		}
		return c.joinTableColumnName(table, ref.name)
	}
	ownNames := make([]string, len(ownRefs))
	invNames := make([]string, len(invRefs))
	for i, ref := range ownRefs {
		ownNames[i] = colName(ownSpec, ownTable, ref)
	}
	for i, ref := range invRefs {
		invNames[i] = colName(invSpec, invTable, ref)
	}
	// changeDuplicatedColumnNames (:272-275 / :287-290): equal names get
	// "_1" and "_2".
	for i := range ownNames {
		for j := range invNames {
			if ownNames[i] == invNames[j] {
				ownNames[i] += "_1"
				invNames[j] += "_2"
			}
		}
	}
	all := append(append([]string(nil), ownNames...), invNames...)
	seen := map[string]bool{}
	for _, n := range all {
		if seen[n] {
			return refuse("two junction columns get the name " + n)
		}
		seen[n] = true
	}
	// The inverse foreign key's actions (1.x A33).
	invDelete, invUpdate := any("CASCADE"), any("CASCADE")
	var invDeleteWhy, invUpdateWhy string
	switch {
	case !inverseKnown:
		invDeleteWhy = "the inverse side is not a literal property"
		invUpdateWhy = invDeleteWhy
	case inverse != "":
		io, why := c.inverseOptions(target, inverse)
		if why != "" {
			invDeleteWhy, invUpdateWhy = why, why
			break
		}
		def := "NO ACTION"
		if c.g.Package.Line == Line1 {
			def = "CASCADE"
		}
		invDelete, invDeleteWhy = actionOrDefault(io.onDelete, io.onDeleteWhy, def)
		invUpdate, invUpdateWhy = actionOrDefault(io.onUpdate, io.onUpdateWhy, def)
	}
	model := identitySegment(name)
	if c.shared[t.e] {
		model = identitySegment(name + "~" + c.g.Root)
	}
	jt := &table{c: c, e: t.e, junction: r}
	jt.rec = &models.Table{
		Identity: "table:" + r.file + "#" + model, File: r.file, Name: name, Model: model,
		ModelLine: r.line, Span: contract.Span{StartLine: r.member.Span.StartLine, EndLine: r.member.Span.EndLine},
		Schema: t.rec.Schema, SchemaKnown: true, ColumnsComplete: true, Test: c.g.Test,
		Attributes: map[string]any{"junction": true, "relation": t.e.Name + "." + r.prop},
	}
	add := func(names []string, refs []*column) []*column {
		var out []*column
		for i, ref := range refs {
			col := &column{t: jt, file: r.file, prop: names[i], line: r.line, member: r.member, name: names[i], nameRow: "TO-R4", primary: true,
				join: &joinInfo{ref: ref, row: "TO-R4", nullable: boolPtr(false)}}
			jt.cols = append(jt.cols, col)
			out = append(out, col)
		}
		return out
	}
	ownCols := add(ownNames, ownRefs)
	invCols := add(invNames, invRefs)
	jt.primaries = len(jt.cols)
	if opts.createFK {
		own := &foreignKey{rel: r, row: "TO-R4", cols: ownCols, refs: ownRefs, target: t, name: firstFKName(ownSpec), file: r.file, line: r.line}
		own.onDelete, own.onDeleteWhy = actionOrDefault(opts.onDelete, opts.onDeleteWhy, "CASCADE")
		own.onUpdate, own.onUpdateWhy = actionOrDefault(opts.onUpdate, opts.onUpdateWhy, "CASCADE")
		inv := &foreignKey{rel: r, row: "TO-R4", cols: invCols, refs: invRefs, target: target, name: firstFKName(invSpec), file: r.file, line: r.line,
			onDelete: invDelete, onDeleteWhy: invDeleteWhy, onUpdate: invUpdate, onUpdateWhy: invUpdateWhy}
		jt.fks = append(jt.fks, own, inv)
	} else {
		for _, col := range jt.cols {
			col.noFK = true
		}
	}
	jt.junctionIndexes = [][]*column{ownCols, invCols}
	return jt
}

func firstFKName(specs []joinSpec) string {
	if len(specs) > 0 {
		return specs[0].fkName
	}
	return ""
}

// inverseOptions reads the options of the target's inverse many-to-many
// relation named property.
func (c *claimer) inverseOptions(target *table, property string) (relationOptions, string) {
	for _, ir := range target.rels {
		if ir.prop != property {
			continue
		}
		if ir.kind != "ManyToMany" {
			return relationOptions{}, "the inverse side " + target.e.Name + "." + property + " is not a @ManyToMany"
		}
		_, _, _, opts, why := ir.args()
		if why != "" {
			return relationOptions{}, why
		}
		o := readRelationOptions(opts, c)
		if o.why != "" {
			return relationOptions{}, o.why
		}
		return o, ""
	}
	return relationOptions{}, "the inverse side " + target.e.Name + "." + property + " is not a read relation"
}

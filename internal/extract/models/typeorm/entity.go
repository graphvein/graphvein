package typeorm

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// This file reads one entity class into a table and its column members
// (rows TO-K2, TO-K6, TO-E1, TO-E2, TO-E3, TO-C1 to TO-C6, TO-P2 and D),
// before the per-dialect rendering of render.go and column.go.

// columnDecorators are typeorm's column decorators (TO-C1, TO-C3).
var columnDecorators = map[string]bool{
	"Column": true, "PrimaryColumn": true, "PrimaryGeneratedColumn": true,
	"CreateDateColumn": true, "UpdateDateColumn": true, "DeleteDateColumn": true, "VersionColumn": true,
}

// wrappedDecorators are the column decorators a TO-C6 wrapper may return.
var wrappedDecorators = map[string]bool{
	"Column": true, "PrimaryColumn": true, "CreateDateColumn": true, "UpdateDateColumn": true, "DeleteDateColumn": true,
}

// noFactDecorators are typeorm member decorators with no table fact
// (TO-E1): relation ids, virtual columns and entity listeners.
var noFactDecorators = map[string]bool{
	"RelationId": true, "RelationCount": true, "VirtualColumn": true,
	"AfterLoad": true, "AfterInsert": true, "AfterUpdate": true, "AfterRemove": true, "AfterSoftRemove": true, "AfterRecover": true,
	"BeforeInsert": true, "BeforeUpdate": true, "BeforeRemove": true, "BeforeSoftRemove": true, "BeforeRecover": true,
}

// noFactPackages are TO-E3's packages: their decorators never touch
// TypeORM metadata.
var noFactPackages = map[string]bool{"@nestjs/swagger": true, "class-transformer": true, "class-validator": true}

// table is one entity's table while it is built.
type table struct {
	c   *claimer
	e   *Entity
	rec *models.Table
	// syncOff is `synchronize: false` (TO-E1, Revision 1 O4): its facts
	// are mapped.
	syncOff bool
	cols    []*column
	// primaries counts the read primary columns (sqlite increment needs
	// the single integer primary key).
	primaries int
	// owners are the owning relations (their join columns are added by
	// the relation pass, relations.go).
	owners []string
	// rels are the relation members (TO-R1 to TO-R4); fks the foreign keys
	// the relation pass built; relUniques the one-to-one owners' unique
	// constraints (TO-R2).
	rels       []*relation
	fks        []*foreignKey
	relUniques []*uniqueSpec
	// indexes, uniques and checks are the TO-X1 to TO-X3 decorators read
	// (constraints.go resolves their columns).
	indexes []*indexSpec
	uniques []*uniqueSpec
	checks  []*checkSpec
	// consOpen makes the constraint set partial (a reason says why);
	// fkOpen makes every column's foreign-key list unknown (a relation
	// that could add a foreign key to any column is not read).
	consOpen, fkOpen bool
	// relFailed says an owning relation's join columns are not read;
	// pkHidden that an unread member may add a primary column.
	relFailed, pkHidden bool
	// junction is the many-to-many owner of a junction table (TO-R4);
	// junctionIndexes its one index per side.
	junction        *relation
	junctionIndexes [][]*column
}

// consPartial makes the constraint set partial.
func (t *table) consPartial(row, file string, line int, format string, args ...any) {
	t.consOpen = true
	t.rec.Reasons = append(t.rec.Reasons, models.Reason{Row: row, Fact: models.Fact("constraint_set"), File: file, Line: line, Reason: fmt.Sprintf(format, args...)})
}

func (t *table) reason(row, fact string, line int, format string, args ...any) {
	var f *string
	if fact != "" {
		f = models.Fact(fact)
	}
	t.rec.Reasons = append(t.rec.Reasons, models.Reason{Row: row, Fact: f, File: t.e.File, Line: line, Reason: fmt.Sprintf(format, args...)})
}

// partial makes the column set partial (a member that may hold a column
// is not read).
func (t *table) partial(row, file string, line int, format string, args ...any) {
	t.pkHidden = true
	t.partialNoKey(row, file, line, format, args...)
}

// treeForms are the D forms that add columns which are never primary
// (@Tree's mpath or closure columns, @TreeParent's join column), so the
// primary key stays known (relation targets).
var treeForms = map[string]bool{"Tree": true, "TreeParent": true, "TreeChildren": true}

// partialNoKey makes the column set partial for a member that cannot add a
// primary column (a tree form, an unread relation's join columns).
func (t *table) partialNoKey(row, file string, line int, format string, args ...any) {
	t.rec.ColumnsComplete = false
	t.rec.Reasons = append(t.rec.Reasons, models.Reason{Row: row, Fact: models.Fact("column_set"), File: file, Line: line, Reason: fmt.Sprintf(format, args...)})
}

func (c *claimer) unread(file string, line int, format string, args ...any) {
	c.steps.Diag(file, "info", codeUnread, fmt.Sprintf("%s:%d: ", file, line)+fmt.Sprintf(format, args...))
}

// entity reads one reached entity class (TO-E1, TO-K2, TO-E2); nil when
// the table is not claimed (a marker says why).
func (c *claimer) entity(e *Entity) *table {
	g := c.g
	line := e.Class.Span.StartLine
	c.tag("TO-E1")
	name, schema, opts, why := c.entityOptions(e)
	if why != "" {
		c.unread(e.File, line, "entity class %s is not claimed: %s", e.Name, why)
		return nil
	}
	syncOff := opts["synchronize"] == "false"
	if name == "" {
		if e.Name == "" {
			c.unread(e.File, line, "an anonymous entity class is not claimed: its table name is not known (TO-K2)")
			return nil
		}
		name = snakeCase(e.Name)
	}
	name = g.EntityPrefix + name
	c.tag(c.namingRow())
	if schema == "" {
		schema = g.Schema
	}
	var schemaPtr *string
	if schema != "" {
		for _, d := range g.Dialects {
			if d != "postgres" {
				c.unread(e.File, line, "entity class %s is not claimed: a schema (%s) on %s is not read (TO-K2)", e.Name, schema, d)
				return nil
			}
		}
		s := schema
		schemaPtr = &s
	}
	model := e.Name
	if c.shared[e] {
		model = identitySegment(e.Name + "~" + g.Root)
	}
	t := &table{c: c, e: e, syncOff: syncOff}
	t.rec = &models.Table{
		Identity: "table:" + e.File + "#" + model, File: e.File, Name: name, Model: model,
		ModelLine: line, Span: e.Class.Span, Schema: schemaPtr, SchemaKnown: true,
		ColumnsComplete: true, Test: g.Test,
		Attributes: map[string]any{"class": e.Name},
	}
	if syncOff {
		t.rec.Verification = models.VerificationMapped
		t.rec.Attributes["synchronize"] = false
		t.reason("TO-E1", "", line, "synchronize: false: TypeORM does not create this table; this is the shape the code expects (Revision 1 O4)")
	}
	for _, k := range sortedKeys(opts) {
		if v := opts[k]; v == "partial" {
			c.tag("D")
			t.rec.ColumnsComplete = false
			t.reason("TO-E1", "column_set", line, "the entity option %s is not read; the facts it can change are unknown", k)
		}
	}
	c.classDecorators(t, e.File, e.Class, true)
	t.members()
	if t.rec.ColumnsComplete && t.primaries == 0 {
		c.fail("(1)", fmt.Sprintf("entity %s (%s:%d) has no primary column (metadata-builder/EntityMetadataValidator.js:48)", e.Name, e.File, line))
	}
	for _, col := range t.cols {
		col.render(t)
	}
	return t
}

// namingRow is the naming row of the group: TO-K2, or TO-K6 with
// SnakeNamingStrategy.
func (c *claimer) namingRow() string {
	if c.g.Naming == "snake" {
		return "TO-K6"
	}
	return "TO-K2"
}

// entityOptions reads the @Entity arguments: the literal table name and
// schema, and the options by key ("true"/"false" for synchronize,
// "partial" for an option whose facts are not read). why says why the
// table is not claimed.
func (c *claimer) entityOptions(e *Entity) (name, schema string, opts map[string]string, why string) {
	opts = map[string]string{}
	dec := e.Decorator
	if dec.Kind != typescript.ModelCall {
		return "", "", opts, ""
	}
	args := dec.Args
	if len(args) > 0 && args[0].Kind != typescript.ModelObject {
		n, ok := stringLiteral(&args[0])
		switch {
		case !ok:
			return "", "", opts, "the @Entity name is not a string literal (TO-K2)"
		case n == "":
			return "", "", opts, "the @Entity name is empty (0.3 falls back to the class name, 1.x keeps it: TO-K2)"
		}
		name = n
		args = args[1:]
	}
	if len(args) > 1 {
		return "", "", opts, "the @Entity arguments are not read"
	}
	if len(args) == 0 {
		return name, "", opts, ""
	}
	if args[0].Kind != typescript.ModelObject {
		return "", "", opts, "the @Entity options are not an object literal"
	}
	for i := range args[0].Entries {
		en := &args[0].Entries[i]
		if en.Kind != typescript.ModelProperty && en.Kind != typescript.ModelShorthand {
			return "", "", opts, fmt.Sprintf("an @Entity option at line %d is not read", en.Span.StartLine)
		}
		switch en.Key {
		case "name":
			n, ok := stringLiteral(&en.Value)
			switch {
			case !ok:
				return "", "", opts, "the @Entity name is not a string literal (TO-K2)"
			case n == "":
				return "", "", opts, "the @Entity name is empty (0.3 falls back to the class name, 1.x keeps it: TO-K2)"
			}
			name = n
		case "schema":
			s, ok := stringLiteral(&en.Value)
			if !ok || s == "" {
				return "", "", opts, "the @Entity schema is not a non-empty string literal (TO-K2)"
			}
			schema = s
		case "synchronize":
			b, ok := boolLiteral(&en.Value)
			if !ok {
				return "", "", opts, "the @Entity synchronize option is not a boolean literal"
			}
			opts["synchronize"] = strconv.FormatBool(b)
		case "orderBy":
			// No table fact (TO-E1).
		case "database":
			c.tag("D")
			return "", "", opts, "the @Entity database option is not read (row D)"
		default:
			opts[en.Key] = "partial"
		}
	}
	return name, schema, opts, ""
}

// decoratorKind classifies a decorator expression of file: "typeorm"
// (name is typeorm's export), "nofact" (a TO-E3 package), "project" (a
// module-level function or const of the repository: a TO-C6 candidate)
// or "other".
type decoratorRef struct {
	kind, name, module string
	b                  binding
	call               *typescript.ModelExpr
}

func (c *claimer) decorator(file string, dec *typescript.ModelExpr) decoratorRef {
	callee := dec
	var call *typescript.ModelExpr
	if dec.Kind == typescript.ModelCall {
		callee, call = dec.Callee, dec
	}
	b := c.d.expr(file, callee)
	label := memberPath(callee)
	switch {
	case b.External == pkgTypeORM:
		return decoratorRef{kind: "typeorm", name: b.Name, call: call, b: b}
	case noFactPackages[b.External]:
		return decoratorRef{kind: "nofact", name: b.Name, module: b.External, call: call, b: b}
	case b.Func != nil || b.Decl != nil:
		return decoratorRef{kind: "project", name: label, call: call, b: b}
	}
	return decoratorRef{kind: "other", name: label, module: b.External, call: call, b: b}
}

// classDecorators reads the class decorators besides @Entity: rows TO-X,
// D forms (@Tree, @TableInheritance, ...), TO-E3 packages
// and project decorators (the column set is then not proven).
func (c *claimer) classDecorators(t *table, file string, cls *typescript.ModelStatement, own bool) {
	e := t.e
	for i := range cls.DecoratorList {
		dec := &cls.DecoratorList[i]
		if dec == e.Decorator {
			continue
		}
		r := c.decorator(file, dec)
		line := dec.Span.StartLine
		switch {
		case r.kind == "typeorm" && (r.name == "Index" || r.name == "Unique" || r.name == "Check"):
			// Rows TO-X1 to TO-X3 (constraints.go): a base class's
			// decorators apply too (filterIndices(inheritanceTree)).
			t.classConstraint(file, dec, r.name)
		case !own:
			// A base class's other decorators are not read here (TOB3).
		case r.kind == "typeorm":
			c.tag("D")
			c.unread(e.File, line, "@%s on entity %s is not read (row D)", r.name, e.Name)
			if treeForms[r.name] {
				t.partialNoKey("D", e.File, line, "@%s is not read (row D): it can add columns", r.name)
			} else {
				t.partial("D", e.File, line, "@%s is not read (row D): it can add columns", r.name)
			}
		case r.kind == "nofact":
			c.tag("TO-E3")
		default:
			c.tag("TO-P2")
			t.partial("TO-P2", e.File, line, "the class decorator %s is not a TypeORM decorator; what it does to the metadata is not read", r.name)
		}
	}
}

// members reads the members of the class and of its bases, base members
// first (TO-E2).
func (t *table) members() {
	c := t.c
	type level struct {
		file string
		cls  *typescript.ModelStatement
	}
	chain := []level{{t.e.File, t.e.Class}}
	seen := map[*typescript.ModelStatement]bool{t.e.Class: true}
	for cur := chain[0]; cur.cls.Extends != nil; {
		ext := cur.cls.Extends
		b := c.d.expr(cur.file, ext)
		if b.External == pkgTypeORM && b.Name == "BaseEntity" {
			c.tag("TO-E2")
			break
		}
		if b.Class == nil || seen[b.Class] {
			c.tag("TO-E2")
			t.partial("TO-E2", cur.file, ext.Span.StartLine, "the base class %s does not resolve to a class of the repository", memberPath(ext))
			break
		}
		c.tag("TO-E2")
		seen[b.Class] = true
		cur = level{b.File, b.Class}
		chain = append([]level{cur}, chain...)
		c.classDecorators(t, b.File, b.Class, false)
	}
	for _, l := range chain {
		for i := range l.cls.Members {
			t.member(l.file, &l.cls.Members[i])
		}
	}
}

// member reads one class member.
func (t *table) member(file string, m *typescript.ModelClassMember) {
	c := t.c
	if len(m.Decorators) == 0 {
		return
	}
	line := m.Span.StartLine
	var cols, wrappers, relations, joins, xdecs, nofact, dforms, project []decoratorRef
	for i := range m.Decorators {
		r := c.decorator(file, &m.Decorators[i])
		switch {
		case r.kind == "typeorm" && columnDecorators[r.name]:
			cols = append(cols, r)
		case r.kind == "typeorm" && relationDecorators[r.name]:
			relations = append(relations, r)
		case r.kind == "typeorm" && (r.name == "JoinColumn" || r.name == "JoinTable"):
			joins = append(joins, r)
		case r.kind == "typeorm" && (r.name == "Index" || r.name == "Unique" || r.name == "Check"):
			xdecs = append(xdecs, r)
		case r.kind == "typeorm" && r.name == "RelationCount" && c.g.Package.Line == Line1:
			c.fail("(8)", fmt.Sprintf("%s:%d: @RelationCount does not exist in typeorm 1.x: the entity module fails to load", file, line))
			nofact = append(nofact, r)
		case r.kind == "typeorm" && noFactDecorators[r.name]:
			nofact = append(nofact, r)
		case r.kind == "typeorm":
			dforms = append(dforms, r)
		case r.kind == "nofact":
			c.tag("TO-E3")
			nofact = append(nofact, r)
		case r.kind == "project":
			wrappers = append(wrappers, r)
		default:
			project = append(project, r)
		}
	}
	// What the member may add besides its own column: join columns and
	// foreign keys (relations), indexes and constraints. When the member
	// is not read, they are not either.
	unread := func(row, format string, args ...any) {
		if len(relations)+len(joins) > 0 {
			t.fkOpen = true
		}
		if len(relations)+len(joins)+len(xdecs) > 0 {
			t.consPartial(row, file, line, format, args...)
		}
	}
	if m.Kind != typescript.MemberProperty {
		if len(cols)+len(relations)+len(joins)+len(xdecs)+len(dforms)+len(wrappers)+len(project) > 0 {
			c.tag("D")
			t.partial("D", file, line, "a decorated method or accessor %s is not read", m.Name)
			unread("D", "a decorated method or accessor %s is not read", m.Name)
		}
		return
	}
	for _, r := range dforms {
		c.tag("D")
		c.unread(file, line, "@%s on %s.%s is not read (row D)", r.name, t.e.Name, m.Name)
	}
	switch {
	case len(project) > 0 || (len(wrappers) > 0 && (len(cols) > 0 || len(wrappers) > 1 || len(relations) > 0)):
		r := project
		if len(r) == 0 {
			r = wrappers
		}
		c.tag("TO-P2")
		if len(cols) == 1 && cols[0].name == "Column" && len(relations) == 0 {
			// The member is a typeorm @Column beside the project
			// decorator: its column is not read, but it is not a primary
			// column, so the primary key stays known (aqualink-app's
			// ApiPointProperty, open point O17).
			t.partialNoKey("TO-P2", file, line, "%s: the decorator %s is not a TypeORM decorator; the member's column is not read", m.Name, r[0].name)
		} else {
			t.partial("TO-P2", file, line, "%s: the decorator %s is not a TypeORM decorator; the member's column is not read", m.Name, r[0].name)
		}
		unread("TO-P2", "%s: the decorator %s is not read, so the member's constraints are not", m.Name, r[0].name)
		return
	case len(dforms) > 0:
		if len(cols)+len(wrappers) > 0 || len(relations) > 0 {
			t.partial("D", file, line, "%s: @%s is not read (row D)", m.Name, dforms[0].name)
			if len(cols) == 1 && len(relations) == 0 {
				col := t.newColumn(file, m, cols[0])
				if col != nil {
					col.unknown("D", "@%s is not read (row D)", dforms[0].name)
				}
			}
			unread("D", "%s: @%s is not read (row D)", m.Name, dforms[0].name)
			return
		}
		if len(dforms) == 1 && treeForms[dforms[0].name] {
			t.partialNoKey("D", file, line, "%s: @%s is not read (row D): it can add a column", m.Name, dforms[0].name)
		} else {
			t.partial("D", file, line, "%s: @%s is not read (row D): it can add a column", m.Name, dforms[0].name)
		}
		unread("D", "%s: @%s is not read (row D)", m.Name, dforms[0].name)
		return
	case len(cols) > 1 || len(cols)+len(wrappers) > 0 && len(relations) > 0 || len(relations) > 1:
		c.tag("D")
		t.partial("D", file, line, "%s: several column or relation decorators on one member are not read", m.Name)
		unread("D", "%s: several column or relation decorators on one member are not read", m.Name)
		return
	case len(joins) > 0 && (len(relations) == 0 || len(joins) > 1 || !joinFits(relations[0].name, joins[0].name)):
		c.tag("D")
		t.partial("D", file, line, "%s: @%s is not read here (row D)", m.Name, joins[0].name)
		unread("D", "%s: @%s is not read here (row D)", m.Name, joins[0].name)
		return
	case len(wrappers) == 1:
		t.wrapped(file, m, wrappers[0])
	case len(cols) == 1:
		t.newColumn(file, m, cols[0])
	case len(relations) == 1:
		r := &relation{t: t, kind: relations[0].name, file: file, prop: m.Name, line: line, member: m, call: relations[0].call}
		if len(joins) == 1 {
			switch joins[0].name {
			case "JoinColumn":
				r.joinColumn = joinExpr(joins[0])
			default:
				r.joinTable = joinExpr(joins[0])
			}
		}
		if r.kind == "ManyToOne" || r.kind == "OneToOne" && r.joinColumn != nil {
			t.owners = append(t.owners, m.Name)
		}
		t.rels = append(t.rels, r)
	}
	for _, r := range xdecs {
		t.propertyConstraint(file, m, r)
	}
}

// relationDecorators are typeorm's relation decorators (TO-R1 to TO-R4).
var relationDecorators = map[string]bool{"ManyToOne": true, "OneToOne": true, "OneToMany": true, "ManyToMany": true}

// joinFits says a @JoinColumn or @JoinTable belongs to the relation kind.
func joinFits(rel, join string) bool {
	if join == "JoinColumn" {
		return rel == "ManyToOne" || rel == "OneToOne"
	}
	return rel == "ManyToMany"
}

// joinExpr is the decorator expression of a @JoinColumn or @JoinTable: the
// call, or a bare identifier for `@JoinColumn` without parentheses.
func joinExpr(r decoratorRef) *typescript.ModelExpr {
	if r.call != nil {
		return r.call
	}
	return &typescript.ModelExpr{Kind: typescript.ModelIdentifier}
}

// wrapped reads a TO-C6 wrapper: a module-level function or const arrow
// of the repository whose last statement returns a typeorm column
// decorator called with the wrapper's single parameter, every earlier
// statement only assigning options.type or options.default (directly or
// inside if statements). Anything else is TO-P2.
func (t *table) wrapped(file string, m *typescript.ModelClassMember, r decoratorRef) {
	c := t.c
	refuse := func(why string) {
		c.tag("TO-P2")
		t.partial("TO-P2", file, m.Span.StartLine, "%s: the decorator %s is not read: %s", m.Name, r.name, why)
	}
	if r.call == nil || len(r.call.Args) != 1 || r.call.Args[0].Kind != typescript.ModelObject {
		refuse("it is not called with one object literal")
		return
	}
	var fn *typescript.ModelExpr
	switch {
	case r.b.Func != nil:
		fn = r.b.Func.Value
	case r.b.Decl != nil && r.b.Decl.Keyword == "const":
		fn = &r.b.Decl.Value
	}
	if fn == nil || (fn.Kind != typescript.ModelFunction && fn.Kind != typescript.ModelArrow) || fn.Async || len(fn.Params) != 1 || fn.Params[0] == "" {
		refuse("it is not a module-level non-async function of one parameter")
		return
	}
	if c.d.owner(r.b.File) != c.g.Package.Dir {
		refuse("it is declared in another package")
		return
	}
	param := fn.Params[0]
	var body []typescript.ModelStatement
	var ret *typescript.ModelExpr
	switch {
	case fn.Kind == typescript.ModelArrow && !fn.Block:
		ret = fn.Body
	case len(fn.Statements) > 0 && fn.Statements[len(fn.Statements)-1].Kind == typescript.StatementReturn:
		body = fn.Statements[:len(fn.Statements)-1]
		ret = fn.Statements[len(fn.Statements)-1].Value
	}
	if ret == nil || ret.Kind != typescript.ModelCall || len(ret.Args) != 1 || ret.Args[0].Kind != typescript.ModelIdentifier || ret.Args[0].Name != param {
		refuse("its last statement is not `return <Column decorator>(" + param + ")`")
		return
	}
	inner := c.decorator(r.b.File, ret)
	if inner.kind != "typeorm" || !wrappedDecorators[inner.name] {
		refuse("it does not return a typeorm column decorator")
		return
	}
	assigns := map[string]bool{}
	if why := wrapperBody(body, param, assigns); why != "" {
		refuse(why)
		return
	}
	c.tag("TO-C6")
	inner.call = r.call
	col := t.newColumn(file, m, inner)
	if col == nil {
		return
	}
	col.wrapper = r.name
	col.wrapType, col.wrapDefault = assigns["type"], assigns["default"]
}

// wrapperBody checks a wrapper's statements before its return: only
// assignments to <param>.type or <param>.default, directly or in if
// statements (seerr's `if (columnOptions.type) columnOptions.type = ...`).
func wrapperBody(list []typescript.ModelStatement, param string, assigns map[string]bool) string {
	for i := range list {
		s := &list[i]
		switch s.Kind {
		case typescript.StatementExpression:
			v := s.Value
			if v.Kind != typescript.ModelAssign || v.Left.Kind != typescript.ModelMember || v.Left.Object.Kind != typescript.ModelIdentifier ||
				v.Left.Object.Name != param || (v.Left.Name != "type" && v.Left.Name != "default") {
				return fmt.Sprintf("its statement at line %d is not an assignment to %s.type or %s.default", s.Span.StartLine, param, param)
			}
			assigns[v.Left.Name] = true
		case typescript.StatementIf:
			if why := wrapperBody(s.Body, param, assigns); why != "" {
				return why
			}
			if why := wrapperBody(s.Else, param, assigns); why != "" {
				return why
			}
		default:
			return fmt.Sprintf("its statement at line %d is not read", s.Span.StartLine)
		}
	}
	return ""
}

// enumMembers computes an S8 enum's runtime values in the order TypeORM
// reads them (the non-numeric keys' values, metadata/ColumnMetadata.js:184):
// numbers for numeric members (auto-increment from the previous number),
// strings for string members. ok is false when a member is not read.
func enumMembers(s *typescript.ModelStatement) (values []defValue, ok bool) {
	next, haveNext := 0, true
	for _, m := range s.EnumMembers {
		if _, err := strconv.Atoi(m.Name); err == nil || m.Name == "" {
			return nil, false
		}
		switch {
		case m.Value == nil:
			if !haveNext {
				return nil, false
			}
			values = append(values, defValue{kind: "number", text: strconv.Itoa(next)})
			next++
		case m.Value.Kind == typescript.ModelNumber:
			n, err := strconv.Atoi(m.Value.Value)
			if err != nil {
				return nil, false
			}
			values = append(values, defValue{kind: "number", text: m.Value.Value})
			next, haveNext = n+1, true
		case m.Value.Kind == typescript.ModelString:
			values = append(values, defValue{kind: "string", text: m.Value.Value})
			haveNext = false
		default:
			return nil, false
		}
	}
	return values, true
}

// snakeRE1 and snakeRE2 are TypeORM's snakeCase (util/StringUtils.js:29-36
// at 0.3.31, :32-39 at 1.1.3).
var (
	snakeRE1 = regexp.MustCompile(`([A-Z])([A-Z])([a-z])`)
	snakeRE2 = regexp.MustCompile(`([a-z0-9])([A-Z])`)
)

func snakeCase(s string) string {
	s = snakeRE1.ReplaceAllString(s, "${1}_${2}${3}")
	s = snakeRE2.ReplaceAllString(s, "${1}_${2}")
	return strings.ToLower(s)
}

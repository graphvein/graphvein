package typeorm

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// This file reads the indexes and constraints of an entity (rows TO-X1 to
// TO-X4): @Index, @Unique and @Check decorators of the class,
// its bases and its properties, column `unique: true` (TO-C4), the
// one-to-one owners' unique join columns (TO-R2), the junction tables'
// indexes (TO-R4) and the foreign keys of the relation pass, with the
// names TypeORM generates when the data source has no migrations
// (Revision 1 O5). Citations are to typeorm 0.3.31 (decorator/Index.js,
// decorator/Unique.js, decorator/Check.js, metadata/IndexMetadata.js,
// metadata/UniqueMetadata.js, metadata-builder/EntityMetadataBuilder.js).

// indexSpec is one @Index decorator.
type indexSpec struct {
	file string
	line int
	// form is "class" or "property"; props the property names (one for the
	// property form).
	form   string
	name   string
	props  []string
	unique bool
	where  string
	// why makes the index unknown; row is its refusal row (TO-X1 or D).
	why, row string
}

// uniqueSpec is one unique constraint: a class @Unique, a column's
// `unique: true` or a one-to-one owner's join columns.
type uniqueSpec struct {
	row        string
	file       string
	line       int
	name       string
	props      []string
	cols       []*column
	rel        bool
	why, whyRw string
}

// checkSpec is one class @Check.
type checkSpec struct {
	file       string
	line       int
	name, expr string
	why        string
}

// classConstraint reads a class-level @Index, @Unique or @Check (rows
// TO-X1 to TO-X3).
func (t *table) classConstraint(file string, dec *typescript.ModelExpr, name string) {
	line := dec.Span.StartLine
	var args []typescript.ModelExpr
	if dec.Kind == typescript.ModelCall {
		args = dec.Args
	}
	switch name {
	case "Index":
		t.indexes = append(t.indexes, t.c.indexDecorator(file, line, args, "class"))
	case "Unique":
		u := &uniqueSpec{row: "TO-X2", file: file, line: line}
		n, fields, opts, why := splitArgs(args)
		u.name, u.why = n, why
		switch {
		case why != "":
		case fields == nil:
			u.why = "the @Unique fields are not an array literal of strings"
		case opts != nil:
			u.why = "the @Unique options are not read (deferrable: row D)"
			u.whyRw = "D"
		default:
			u.props, u.why = stringList(fields)
		}
		t.uniques = append(t.uniques, u)
	case "Check":
		ch := &checkSpec{file: file, line: line}
		switch len(args) {
		case 1:
			ch.expr, ch.why = literalArg(&args[0], "the @Check expression")
		case 2:
			ch.name, ch.why = literalArg(&args[0], "the @Check name")
			if ch.why == "" {
				ch.expr, ch.why = literalArg(&args[1], "the @Check expression")
			}
		default:
			ch.why = "the @Check arguments are not read"
		}
		t.checks = append(t.checks, ch)
	}
}

// propertyConstraint reads a property-level @Index (TO-X1); a property
// @Unique or @Check is D (the constraint set is partial).
func (t *table) propertyConstraint(file string, m *typescript.ModelClassMember, r decoratorRef) {
	line := m.Span.StartLine
	if r.name != "Index" {
		t.c.tag("D")
		t.c.unread(file, line, "a property-level @%s on %s.%s is not read (row D)", r.name, t.e.Name, m.Name)
		t.consPartial("D", file, line, "%s: a property-level @%s is not read (row D)", m.Name, r.name)
		return
	}
	var args []typescript.ModelExpr
	if r.call != nil {
		args = r.call.Args
	}
	ix := t.c.indexDecorator(file, line, args, "property")
	ix.props = []string{m.Name}
	t.indexes = append(t.indexes, ix)
}

// splitArgs splits @Index/@Unique arguments as decorator/Index.js:17-42
// does: an optional string name, then the fields (an array literal, or a
// function), then an options object.
func splitArgs(args []typescript.ModelExpr) (name string, fields, opts *typescript.ModelExpr, why string) {
	i := 0
	if i < len(args) && args[i].Kind != typescript.ModelArray && args[i].Kind != typescript.ModelObject && args[i].Kind != typescript.ModelArrow {
		s, ok := stringLiteral(&args[i])
		if !ok || s == "" {
			return "", nil, nil, "the name is not a non-empty string literal"
		}
		name = s
		i++
	}
	if i < len(args) && args[i].Kind != typescript.ModelObject {
		fields = &args[i]
		i++
	}
	if i < len(args) {
		if args[i].Kind != typescript.ModelObject {
			return "", nil, nil, "an argument is not read"
		}
		opts = &args[i]
		i++
	}
	if i < len(args) {
		return "", nil, nil, "the arguments are not read"
	}
	return name, fields, opts, ""
}

func stringList(e *typescript.ModelExpr) ([]string, string) {
	if e.Kind != typescript.ModelArray || len(e.Elements) == 0 {
		return nil, "the fields are not a non-empty array literal"
	}
	var out []string
	for i := range e.Elements {
		s, ok := stringLiteral(&e.Elements[i])
		if !ok || s == "" {
			return nil, "a field is not a string literal"
		}
		out = append(out, strings.TrimSpace(s))
	}
	return out, ""
}

func literalArg(e *typescript.ModelExpr, what string) (string, string) {
	s, ok := stringLiteral(e)
	if !ok || s == "" {
		return "", what + " is not a non-empty string literal"
	}
	return s, ""
}

// indexOptions are the @Index options of TO-X1's refusal column: each
// makes the index unknown.
var indexRefusedOptions = map[string]bool{
	"spatial": true, "fulltext": true, "parser": true, "sparse": true, "background": true, "expireAfterSeconds": true,
}

// indexDecorator reads @Index arguments (TO-X1).
func (c *claimer) indexDecorator(file string, line int, args []typescript.ModelExpr, form string) *indexSpec {
	ix := &indexSpec{file: file, line: line, form: form, row: "TO-X1"}
	name, fields, opts, why := splitArgs(args)
	ix.name = name
	c.indexTypeFailure(file, line, opts)
	switch {
	case why != "":
		ix.why = "the @Index " + why
		return ix
	case fields != nil && fields.Kind == typescript.ModelArrow:
		ix.why = "the function form of @Index is not read"
		return ix
	case fields != nil && form == "property":
		ix.why = "a property-level @Index with fields is not read"
		return ix
	case fields != nil:
		ix.props, ix.why = stringList(fields)
		if ix.why != "" {
			ix.why = "the @Index " + ix.why
			return ix
		}
	}
	if why := c.indexOptions(ix, opts); why != "" {
		ix.why = why
		return ix
	}
	if form == "class" && fields == nil {
		// Read after the options: `@Index("name", {synchronize: false})`
		// is the synchronize refusal.
		ix.why = "a class-level @Index without fields is not read"
	}
	return ix
}

// indexTypeFailure applies TO-P1 (9): under 1.x, IndexMetadata's
// constructor throws "Unsupported index type" for a truthy `type` option
// when the driver has no supportedIndexTypes list
// (metadata/IndexMetadata.js:49-52 at 1.1.2 and 1.1.3), and only
// PostgresDriver has one, so initialize() fails on every other dialect.
// The constructor checks it before any other option, so it applies
// whatever else the @Index holds. A non-literal type is assumed not to
// fail (TO-V2); on postgres the index stays a TO-X1 refusal.
func (c *claimer) indexTypeFailure(file string, line int, opts *typescript.ModelExpr) {
	if c.g.Package.Line != Line1 || opts == nil || opts.Kind != typescript.ModelObject {
		return
	}
	for i := range opts.Entries {
		en := &opts.Entries[i]
		if en.Kind != typescript.ModelProperty || en.Key != "type" {
			continue
		}
		if s, ok := stringLiteral(&en.Value); !ok || s == "" {
			return
		}
		var dialects []string
		for _, d := range c.g.Dialects {
			if d != "postgres" {
				dialects = append(dialects, d)
			}
		}
		if len(dialects) > 0 {
			c.fail("(9)", fmt.Sprintf("%s:%d: an @Index type option throws in typeorm 1.x on a driver without index types (IndexMetadata.js:49-52)", file, line), dialects...)
		}
		return
	}
}

// indexOptions reads the @Index options into ix; it returns why the index
// is not read (ix.row names the refusal row).
func (c *claimer) indexOptions(ix *indexSpec, opts *typescript.ModelExpr) string {
	if opts == nil {
		return ""
	}
	for i := range opts.Entries {
		en := &opts.Entries[i]
		if en.Kind != typescript.ModelProperty {
			return fmt.Sprintf("an @Index option at line %d is not read", en.Span.StartLine)
		}
		switch {
		case en.Key == "unique":
			b, ok := boolLiteral(&en.Value)
			if !ok {
				return "the @Index unique option is not a boolean literal"
			}
			ix.unique = b
		case en.Key == "where":
			s, ok := stringLiteral(&en.Value)
			if !ok || s == "" {
				return "the @Index where option is not a non-empty string literal"
			}
			ix.where = s
		case en.Key == "synchronize":
			b, ok := boolLiteral(&en.Value)
			switch {
			case !ok:
				return "the @Index synchronize option is not a boolean literal"
			case !b:
				// TypeORM never creates the index (TO-X1 refusal).
				return "synchronize: false (TypeORM does not create this index)"
			}
		case en.Key == "type" && c.g.Package.Line == Line1:
			return "the 1.x index type option is not read (TO-X1)"
		case indexRefusedOptions[en.Key]:
			return "the @Index option " + en.Key + " is not read (TO-X1)"
		default:
			ix.row = "D"
			return "the @Index option " + en.Key + " is not read (row D)"
		}
	}
	return ""
}

// propertyColumns resolves property names to column names as
// IndexMetadata.build and UniqueMetadata.build do (:35-48): a column of
// that property, else a relation's join columns.
func (t *table) propertyColumns(props []string) ([]string, string) {
	var out []string
	for _, p := range props {
		var found *column
		for _, col := range t.cols {
			if col.prop == p && col.join == nil {
				found = col
			}
		}
		if found == nil {
			for _, r := range t.rels {
				if r.prop != p {
					continue
				}
				switch {
				case r.joinCols == nil:
					return nil, "the relation " + p + "'s join columns are not read"
				case len(r.joinCols) != 1:
					return nil, "the relation " + p + " has several join columns (not read)"
				}
				found = r.joinCols[0]
			}
		}
		if found == nil {
			return nil, "the property " + p + " is not a read column or relation"
		}
		if found.unknownWhy != "" {
			return nil, "the column " + found.name + " is not read"
		}
		out = append(out, found.name)
	}
	return out, ""
}

// generatedNames says whether TO-X4 claims generated names: the data
// source states no migrations option and the package holds no migrations
// folder (Revision 1 O5; names drift in migration-built
// databases).
func (c *claimer) generatedNames() bool {
	if c.g.HasMigrations {
		return false
	}
	if c.noMigrations == nil {
		found := false
		dir := c.g.Package.Dir
		for f := range c.d.in.Sources {
			if dir != "." && !strings.HasPrefix(f, dir+"/") || underNodeModules(f) {
				continue
			}
			for _, seg := range strings.Split(path.Dir(f), "/") {
				if seg == "migration" || seg == "migrations" {
					found = true
				}
			}
		}
		v := !found
		c.noMigrations = &v
	}
	return *c.noMigrations
}

// constraintName is a constraint's or index's name: the explicit one, or
// the generated one when TO-X4 applies and every live dialect generates
// the same (gen returns the name on a dialect, "" when that dialect's
// name is not modelled); nil otherwise.
func (t *table) constraintName(explicit, row string, gen func(d string) string) any {
	c := t.c
	if explicit != "" {
		c.tag(row)
		return explicit
	}
	if !c.generatedNames() || strings.Contains(t.rec.Name, ".") {
		return nil
	}
	var name string
	for i, d := range c.dialects {
		n := gen(d)
		if n == "" || i > 0 && n != name {
			return nil
		}
		name = n
	}
	if name == "" {
		return nil
	}
	c.tag("TO-X4")
	return name
}

func mysqlFamily(d string) bool { return d == "mysql" || d == "mariadb" }

// constraints writes the table's indexes, unique and check constraints and
// its columns' foreign keys, index and unique facts (after finish wrote the
// column records), and the constraint set's completeness.
func (t *table) constraints(recs map[*column]*models.Column) {
	c := t.c
	line := t.rec.ModelLine
	if slices.Contains(c.dialects, "mssql") {
		t.rec.ElementsUnknown = true
		t.consPartial("TO-X2", t.e.File, line, "mssql constraint and index forms are not in the contract's mapped forms (a one-to-one unique is a filtered index there); not listed")
		for _, rec := range recs {
			rec.ForeignKeysUnknown = true
			rec.Reasons = append(rec.Reasons, models.Reason{Row: "TO-R1", Fact: models.Fact("foreign_keys"), File: rec.File, Line: rec.Line, Reason: "mssql foreign keys are not listed"})
		}
		t.rec.ConstraintsComplete = false
		return
	}
	table := t.rec.Name
	indexed := map[string]bool{}
	uniqueCol := map[string]bool{}
	// Indexes (TO-X1) and the junction's one index per side (TO-R4).
	for _, ix := range t.indexes {
		c.tag(ix.row)
		cols, why := []string(nil), ix.why
		if why == "" {
			cols, why = t.propertyColumns(ix.props)
		}
		if why == "" && ix.where != "" && !slices.Contains(c.dialects, "postgres") && !slices.Contains(c.dialects, "sqlite") {
			why = "a partial index is read on postgres and sqlite only"
		}
		if why == "" && ix.where != "" {
			for _, d := range c.dialects {
				if mysqlFamily(d) {
					why = "MySQL has no partial index (the where option is not mapped)"
				}
			}
		}
		if why != "" {
			c.unread(ix.file, ix.line, "an @Index on %s is not read: %s", t.e.Name, why)
			t.consPartial(ix.row, ix.file, ix.line, "an @Index is not read: %s", why)
			continue
		}
		where := ix.where
		name := t.constraintName(ix.name, "TO-X1", func(string) string { return c.generatedName("IDX_", table, cols, where) })
		t.rec.Indexes = append(t.rec.Indexes, map[string]any{"name": name, "columns": cols, "unique": ix.unique, "partial": where != ""})
		if len(cols) == 1 {
			switch {
			case !ix.unique:
				indexed[cols[0]] = true
			case where == "":
				uniqueCol[cols[0]] = true
			}
		}
	}
	for _, side := range t.junctionIndexes {
		c.tag("TO-R4")
		var cols []string
		for _, col := range side {
			cols = append(cols, col.name)
		}
		name := t.constraintName("", "TO-R4", func(string) string { return c.generatedName("IDX_", table, cols, "") })
		t.rec.Indexes = append(t.rec.Indexes, map[string]any{"name": name, "columns": cols, "unique": false, "partial": false})
		if len(cols) == 1 {
			indexed[cols[0]] = true
		}
	}
	// Unique constraints: class @Unique (TO-X2), column unique: true (TO-C4,
	// TO-X2), one-to-one owners (TO-R2). On MySQL TypeORM writes each as a
	// unique index (EntityMetadataBuilder.js:549-570, :105-130), which MySQL
	// reports as a unique constraint; its generated name is then IDX_ (REL_
	// for a one-to-one).
	var specs []*uniqueSpec
	specs = append(specs, t.uniques...)
	for _, col := range t.cols {
		if col.join == nil && col.unknownWhy == "" && col.unique != nil && *col.unique {
			specs = append(specs, &uniqueSpec{row: "TO-X2", file: col.file, line: col.line, cols: []*column{col}})
		}
	}
	specs = append(specs, t.relUniques...)
	for _, u := range specs {
		c.tag(u.row)
		var cols []string
		why := u.why
		switch {
		case why != "":
		case u.cols != nil:
			for _, col := range u.cols {
				cols = append(cols, col.name)
			}
		default:
			cols, why = t.propertyColumns(u.props)
		}
		if why != "" {
			row := u.row
			if u.whyRw != "" {
				row = u.whyRw
				c.tag(row)
			}
			c.unread(u.file, u.line, "a unique constraint on %s is not read: %s", t.e.Name, why)
			t.consPartial(row, u.file, u.line, "a unique constraint is not read: %s", why)
			continue
		}
		rel := u.rel
		name := t.constraintName(u.name, u.row, func(d string) string {
			switch {
			case rel:
				return c.generatedName("REL_", table, cols, "")
			case mysqlFamily(d):
				return c.generatedName("IDX_", table, cols, "")
			}
			return c.generatedName("UQ_", table, cols, "")
		})
		t.rec.Uniques = append(t.rec.Uniques, map[string]any{"name": name, "columns": cols})
		if len(cols) == 1 {
			uniqueCol[cols[0]] = true
		}
	}
	// Check constraints (TO-X3): never created on MySQL
	// (RdbmsSchemaBuilder.js:694-698); sqlite's simple-enum columns add an
	// unnamed CHECK (TO-C5).
	for _, ch := range t.checks {
		c.tag("TO-X3")
		why := ch.why
		for _, d := range c.dialects {
			if why == "" && mysqlFamily(d) {
				why = "TypeORM does not create check constraints on MySQL (RdbmsSchemaBuilder.js:694-698)"
			}
		}
		if why != "" {
			c.unread(ch.file, ch.line, "a @Check on %s is not read: %s", t.e.Name, why)
			t.consPartial("TO-X3", ch.file, ch.line, "a @Check is not read: %s", why)
			continue
		}
		expr := ch.expr
		name := t.constraintName(ch.name, "TO-X3", func(string) string { return c.checkName(table, expr) })
		t.rec.Checks = append(t.rec.Checks, map[string]any{"name": name, "expression": expr})
	}
	if slices.Contains(c.dialects, "sqlite") {
		for _, col := range t.cols {
			if col.join != nil || col.unknownWhy != "" || col.explicit != "simple-enum" {
				continue
			}
			c.tag("TO-C5")
			if len(c.dialects) > 1 || col.enum == nil {
				t.consPartial("TO-C5", col.file, col.line, "the simple-enum column %s has a CHECK constraint on sqlite only (or its labels are not read); not listed", col.name)
				continue
			}
			var labels []string
			for _, v := range col.enum {
				labels = append(labels, "'"+v.text+"'")
			}
			// AbstractSqliteQueryRunner.js:1306-1312: inline and unnamed.
			t.rec.Checks = append(t.rec.Checks, map[string]any{"name": nil, "expression": `"` + col.name + `" IN (` + strings.Join(labels, ",") + `)`})
		}
	}
	// Foreign keys (TO-R1, TO-R2, TO-R4).
	fkOf := map[*column][]*models.ForeignKey{}
	for _, fk := range t.fks {
		var local, refs []string
		for i, col := range fk.cols {
			local = append(local, col.name)
			refs = append(refs, fk.refs[i].name)
		}
		lc := local
		name := t.constraintName(fk.name, fk.row, func(string) string { return c.generatedName("FK_", table, lc, "") })
		var namePtr *string
		if s, ok := name.(string); ok {
			namePtr = &s
		}
		var schema *string
		if fk.target.rec.Schema != nil {
			s := *fk.target.rec.Schema
			schema = &s
		}
		for i, col := range fk.cols {
			fkOf[col] = append(fkOf[col], &models.ForeignKey{
				Schema: schema, Table: fk.target.rec.Name, Column: refs[i], OnDelete: fk.onDelete, OnUpdate: fk.onUpdate,
				Columns: append([]string(nil), local...), RefColumns: append([]string(nil), refs...), Name: namePtr,
				File: fk.file, Line: fk.line,
			})
		}
		for _, why := range []string{fk.onDeleteWhy, fk.onUpdateWhy} {
			if why != "" {
				t.consPartial(fk.row, fk.file, fk.line, "a foreign key action of %s is not read: %s", fk.rel.prop, why)
			}
		}
	}
	// An unread column or member may add indexes and constraints (TO-P2,
	// D): the set is then partial, and the foreign-key lists of the
	// columns no read relation owns are unknown.
	for _, col := range t.cols {
		switch {
		case col.unknownWhy != "":
			t.consPartial(col.unknownRow, col.file, col.line, "the column %s is not read, so its constraints are not", col.name)
		case col.uniqueWhy != "":
			t.consPartial("TO-C4", col.file, col.line, "%s: %s", col.name, col.uniqueWhy)
		}
	}
	if !t.rec.ColumnsComplete {
		t.consPartial("TO-E1", t.e.File, line, "the column set is partial: a member that is not read may add indexes, constraints or foreign keys")
	}
	fkIndexed := map[string]bool{}
	if !t.consOpen {
		t.foreignKeyIndexes(indexed, fkIndexed)
	}
	complete := !t.consOpen
	for col, rec := range recs {
		if rec.Unknown {
			continue
		}
		switch {
		case !t.rec.ColumnsComplete && col.join == nil && col.mergedRef == nil:
			rec.ForeignKeysUnknown = true
			rec.Reasons = append(rec.Reasons, models.Reason{Row: "TO-R1", Fact: models.Fact("foreign_keys"), File: rec.File, Line: rec.Line, Reason: "the column set is partial: a member that is not read may add a foreign key to this column"})
		case t.fkOpen:
			rec.ForeignKeysUnknown = true
			rec.Reasons = append(rec.Reasons, models.Reason{Row: "TO-R1", Fact: models.Fact("foreign_keys"), File: rec.File, Line: rec.Line, Reason: "a relation of the entity is not read, so it may add a foreign key to this column"})
		case col.fkWhy != "":
			rec.ForeignKeysUnknown = true
			rec.Reasons = append(rec.Reasons, models.Reason{Row: "TO-R1", Fact: models.Fact("foreign_keys"), File: rec.File, Line: rec.Line, Reason: col.fkWhy})
		default:
			rec.ForeignKeysUnknown = false
			rec.ForeignKeys = fkOf[col]
			if row := fkRow(col); row != "" {
				c.tag(row)
				rec.Claims["foreign_keys"] = models.ClaimSource{Row: row, Verification: t.flatVerification()}
			}
		}
		t.indexFacts(col, rec, indexed[col.name], fkIndexed[col.name], uniqueCol[col.name], complete)
	}
	t.rec.ElementsUnknown = false
	t.rec.ConstraintsComplete = complete
}

// foreignKeyIndexes adds the index MySQL's InnoDB creates for a foreign
// key whose columns no index leads with (Revision 4 A3, row TO-R1): "In
// the referencing table, there must be an index where the foreign key
// columns are listed as the first columns in the same order. Such an index
// is created on the referencing table automatically if it does not exist"
// and is named after the CONSTRAINT symbol when one is given (MySQL
// reference manual, "FOREIGN KEY Constraints"); TypeORM always names the
// constraint (MysqlQueryRunner.js:1881, :2004) and itself reads such an
// index back as the foreign key's (it drops every index named like a
// referential constraint, MysqlQueryRunner.js:1507-1520). The index is
// claimed (name: the foreign key's, mapped) on a MySQL-only group whose
// constraint set is otherwise complete; on MariaDB, or with another
// dialect in the set, the set is partial (the index's name, or its
// existence on every dialect, is not proven). Covering: a declared index,
// a unique constraint (a unique index on MySQL) or the primary key whose
// leading columns are the foreign key's in order.
func (t *table) foreignKeyIndexes(indexed, fkIndexed map[string]bool) {
	c := t.c
	mysqlOnly, anyMySQL := true, false
	for _, d := range c.dialects {
		if d == "mysql" || d == "mariadb" {
			anyMySQL = true
		}
		if d != "mysql" {
			mysqlOnly = false
		}
	}
	if !anyMySQL || len(t.fks) == 0 {
		return
	}
	leads := func(cols []string, local []string) bool {
		if len(cols) < len(local) {
			return false
		}
		for i := range local {
			if cols[i] != local[i] {
				return false
			}
		}
		return true
	}
	strs := func(v any) []string {
		out, _ := v.([]string)
		return out
	}
	var pk []string
	for _, col := range t.cols {
		if col.primary {
			pk = append(pk, col.name)
		}
	}
	table := t.rec.Name
	for _, fk := range t.fks {
		var local []string
		for _, col := range fk.cols {
			local = append(local, col.name)
		}
		covered := leads(pk, local)
		for _, ix := range t.rec.Indexes {
			covered = covered || leads(strs(ix["columns"]), local)
		}
		for _, u := range t.rec.Uniques {
			covered = covered || leads(strs(u["columns"]), local)
		}
		if covered {
			continue
		}
		c.tag("TO-R1")
		inPK := 0
		for _, l := range local {
			if slices.Contains(pk, l) {
				inPK++
			}
		}
		switch {
		case inPK == len(local):
			t.consPartial("TO-R1", fk.file, fk.line, "the foreign key of %s is on primary key columns in an order the primary key may lead with; MySQL's own index for it is not read", fk.rel.prop)
		case !mysqlOnly:
			t.consPartial("TO-R1", fk.file, fk.line, "MySQL and MariaDB add an index for the foreign key of %s (InnoDB); its name on MariaDB, or its absence on the other dialects of the set, is not read", fk.rel.prop)
		default:
			lc := local
			name := t.constraintName(fk.name, fk.row, func(string) string { return c.generatedName("FK_", table, lc, "") })
			t.rec.Indexes = append(t.rec.Indexes, map[string]any{"name": name, "columns": local, "unique": false, "partial": false})
			if len(local) == 1 && !indexed[local[0]] {
				indexed[local[0]], fkIndexed[local[0]] = true, true
			}
		}
	}
}

// fkRow is the claim row of a column's foreign-key list: its relation's
// row, else TO-R1 (the list is proven empty because every relation is
// read).
func fkRow(col *column) string {
	switch {
	case col.join != nil:
		return col.join.row
	case col.mergedRow != "":
		return col.mergedRow
	}
	return "TO-R1"
}

// flatVerification is the verification of a table's dialect-independent
// facts.
func (t *table) flatVerification() string {
	if t.syncOff {
		return models.VerificationMapped
	}
	return t.c.weakest()
}

// indexFacts claims a column's index and unique facts: true from a
// single-column index (row TO-R1 when only MySQL's foreign key index
// covers it) or unique constraint, false when the constraint set
// is complete, else unknown with a reason.
func (t *table) indexFacts(col *column, rec *models.Column, indexed, fkIndex, unique, complete bool) {
	c := t.c
	ver := t.flatVerification()
	set := func(fact string, v bool, row string) {
		c.tag(row)
		rec.Attributes[fact] = v
		rec.Claims[fact] = models.ClaimSource{Row: row, Verification: ver}
	}
	switch {
	case fkIndex:
		// MySQL's own index for a foreign key (foreignKeyIndexes).
		set("index", true, "TO-R1")
	case indexed:
		set("index", true, "TO-X1")
	case complete:
		set("index", false, "TO-X1")
	default:
		rec.Reasons = append(rec.Reasons, models.Reason{Row: "TO-X1", Fact: models.Fact("index"), File: rec.File, Line: rec.Line, Reason: "the constraint set is partial, so an index may cover the column"})
	}
	if _, has := rec.Claims["unique"]; has {
		return
	}
	switch {
	case unique:
		set("unique", true, "TO-X2")
	case complete:
		set("unique", false, "TO-X2")
	default:
		rec.Reasons = append(rec.Reasons, models.Reason{Row: "TO-X2", Fact: models.Fact("unique"), File: rec.File, Line: rec.Line, Reason: "the constraint set is partial, so a unique constraint may cover the column"})
	}
}

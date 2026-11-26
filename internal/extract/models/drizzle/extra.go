package drizzle

import (
	"strings"

	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// Row DR-X1: a table builder's third argument, `(t) => [ ... ]` or the
// deprecated `(t) => ({ ... })`, as drizzle-orm's getTableConfig reads it
// (pg-core/utils.js:26-29: an array is flattened one level, an object's
// values are taken) and drizzle-kit serializes it (pgSerializer,
// bin.cjs:17083-17240; sqlite :18668-18760; mysql :16090-16230).

// pkSpec is a primaryKey({columns, name?}) element.
type pkSpec struct {
	cols []*column
	// name is the literal name; nameKnown false when it is not literal.
	name      *string
	nameKnown bool
	line      int
}

// uniqueSpec is a unique(name?).on(...) element.
type uniqueSpec struct {
	cols             []*column
	name             *string
	nameKnown        bool
	nullsNotDistinct bool
	line             int
}

// fkSpec is a foreign key waiting for the group's tables: a column's
// .references() (row DR-C2) or a foreignKey({...}) element (row DR-X1).
type fkSpec struct {
	row  string
	file string
	line int
	cols []*column
	// target names the referenced table: a table const (or the extra
	// config's parameter, the table itself) and the property keys.
	targetFile, targetName string
	self                   bool
	targetKeys             []string
	onDelete, onUpdate     any
	name                   *string
	nameKnown              bool
}

// extraConfig reads the table's third argument. Anything no form of row
// DR-X1 covers is D for that element: the constraint set is partial and
// the column absences it could change stay unknown (t.open).
func (t *table) extraConfig(e typescript.ModelExpr) {
	line := e.Span.StartLine
	if e.Kind != typescript.ModelArrow || e.Block || e.Body == nil || len(e.Params) > 1 || len(e.Params) == 1 && e.Params[0] == "" {
		t.extraUnread(line, "the extra config is not an arrow with one named parameter and an array or object body")
		return
	}
	param := ""
	if len(e.Params) == 1 {
		param = e.Params[0]
	}
	var elements []typescript.ModelExpr
	switch body := *e.Body; body.Kind {
	case typescript.ModelArray:
		for _, el := range body.Elements {
			if el.Kind == typescript.ModelArray {
				// getTableConfig flattens one level (Array.flat(1)).
				elements = append(elements, el.Elements...)
				continue
			}
			elements = append(elements, el)
		}
	case typescript.ModelObject:
		for _, en := range body.Entries {
			if en.Kind != typescript.ModelProperty {
				t.extraUnread(en.Span.StartLine, "a %s entry of the extra config object is not read", en.Kind)
				continue
			}
			elements = append(elements, en.Value)
		}
	default:
		t.extraUnread(line, "the extra config's body is not an array or object literal")
		return
	}
	for _, el := range elements {
		t.extraElement(param, el)
	}
}

// extraUnread marks one element (or the whole extra config) as D.
func (t *table) extraUnread(line int, format string, a ...any) {
	t.g.tag("D")
	t.open = true
	t.rec.ConstraintsComplete = false
	t.rec.PrimaryKeyOpen = true
	t.reason("D", "constraint_set", line, "extra config: "+format, a...)
}

// extraElement reads one element of the extra config.
func (t *table) extraElement(param string, el typescript.ModelExpr) {
	g := t.g
	line := el.Span.StartLine
	base, links, ok := chain(el)
	if !ok {
		t.extraUnread(line, "the element is not a call of a builder of the dialect module")
		return
	}
	r := g.in.calleeRef(t.file, base.Callee)
	if coreDialect[r.Module] != g.dialect {
		t.extraUnread(line, "the element is not a call of a builder of the %s module", g.dialect)
		return
	}
	switch r.Export {
	case "index", "uniqueIndex":
		t.extraIndex(param, base, links, r.Export == "uniqueIndex", line)
	case "primaryKey":
		t.extraPrimaryKey(param, base, links, line)
	case "unique":
		t.extraUnique(param, base, links, line)
	case "foreignKey":
		t.extraForeignKey(param, base, links, line)
	case "check":
		t.extraCheck(param, base, links, line)
	default:
		t.extraUnread(line, "%s is not an element row DR-X1 reads", r.Export)
	}
}

// columnArg reads a column argument `t.col` with its index modifiers
// (`.asc()`, `.desc()`, `.nullsFirst()`, `.nullsLast()`, `.op("cls")`,
// pg-core/columns/common.js:71-130). why is set when it is not one.
func (t *table) columnArg(param string, e typescript.ModelExpr) (c *column, mods []link, why string) {
	for e.Kind == typescript.ModelCall && e.Callee != nil && e.Callee.Kind == typescript.ModelMember && e.Callee.Object != nil {
		mods = append([]link{{Name: e.Callee.Name, Args: e.Args, Line: e.Span.StartLine}}, mods...)
		e = *e.Callee.Object
	}
	if e.Kind != typescript.ModelMember || e.Object == nil || e.Object.Kind != typescript.ModelIdentifier || param == "" || e.Object.Name != param {
		return nil, nil, "a column argument is not a property of the extra config's parameter"
	}
	c, why = t.columnByKey(e.Name)
	return c, mods, why
}

// columnByKey finds the column with property key: a missing key on a
// complete column set is a DR-P1 failure (drizzle-orm reads a property of
// undefined), on a partial one unknown.
func (t *table) columnByKey(key string) (*column, string) {
	for _, c := range t.columns {
		if c.key == key {
			if c.rec.Unknown {
				return nil, "column " + key + " is not known"
			}
			return c, ""
		}
	}
	if t.rec.ColumnsComplete {
		t.missing = append(t.missing, key)
		return nil, "column " + key + " is not a column of the table (DR-P1)"
	}
	return nil, "column " + key + " is not a known column of the table"
}

// extraColumns reads a list of plain column arguments (no modifiers).
func (t *table) extraColumns(param string, args []typescript.ModelExpr) ([]*column, string) {
	var cols []*column
	for _, a := range args {
		c, mods, why := t.columnArg(param, a)
		switch {
		case why != "":
			return nil, why
		case len(mods) > 0:
			return nil, "a column argument with ." + mods[0].Name + "() outside an index"
		}
		cols = append(cols, c)
	}
	if len(cols) == 0 {
		return nil, "no columns"
	}
	return cols, ""
}

// optionalName reads a builder's optional literal name argument.
func optionalName(args []typescript.ModelExpr) (*string, bool) {
	if len(args) == 0 || args[0].Kind == typescript.ModelIdentifier && args[0].Name == "undefined" {
		return nil, true
	}
	s, ok := literalString(args[0])
	if !ok {
		return nil, false
	}
	return &s, true
}

// casedNames returns the database names of cols.
func casedNames(cols []*column) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c.rec.Name
	}
	return out
}

// builderNames returns the names drizzle-orm's columns hold (literal
// name, else the pre-casing key): generated constraint names use
// them.
func builderNames(cols []*column) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c.builderName
	}
	return out
}

// extraIndex reads index(name?) / uniqueIndex(name?) with .on(...) or
// .using(method, ...), then .where(sql), .concurrently(), .with({...}).
func (t *table) extraIndex(param string, base typescript.ModelExpr, links []link, unique bool, line int) {
	g := t.g
	d := g.dialect
	form := "index"
	if unique {
		form = "uniqueIndex"
	}
	name, nameOK := optionalName(base.Args)
	if !nameOK || len(base.Args) > 1 {
		t.extraUnread(line, "%s: the name is not a string literal", form)
		return
	}
	if len(links) == 0 || links[0].Name != "on" && !(links[0].Name == "using" && d == "postgresql") {
		t.extraUnread(line, "%s without .on(...) or .using(...) is not read", form)
		return
	}
	args := links[0].Args
	var method any
	switch {
	case links[0].Name == "using":
		m, ok := literalString(argAt(args, 0))
		if !ok || len(args) < 2 {
			t.extraUnread(line, "%s.using: the method is not a string literal", form)
			return
		}
		method, args = m, args[1:]
	case d == "postgresql":
		method = "btree"
	}
	if len(args) == 0 {
		t.extraUnread(line, "%s with no columns", form)
		return
	}
	cols := make([]any, len(args))
	exprs := make([]any, len(args))
	orders := make([]any, len(args))
	nulls := make([]any, len(args))
	opclasses := make([]any, len(args))
	var plain []*column
	seen := map[*column]bool{}
	hasExpr := false
	for i, a := range args {
		if a.Kind == typescript.ModelTagged {
			hasExpr = true
			if d == "mysql" {
				t.extraUnread(line, "%s: an sql expression column on mysql is not read", form)
				return
			}
			text, _ := t.renderSQL(param, a, false)
			exprs[i], orders[i], nulls[i] = text, "asc", "last"
			continue
		}
		c, mods, why := t.columnArg(param, a)
		if why != "" {
			t.extraUnread(line, "%s: %s", form, why)
			return
		}
		if seen[c] {
			// .on() resets the column's index config after the first use
			// (pg-core/indexes.js:14-16), so a repeated column is not read.
			t.extraUnread(line, "%s: column %s is listed twice", form, c.key)
			return
		}
		seen[c] = true
		order, nl := "asc", "last"
		var opclass any
		for _, m := range mods {
			switch {
			case d != "postgresql":
				t.extraUnread(line, "%s: .%s() on a %s index column is not read", form, m.Name, d)
				return
			case m.Name == "asc" && len(m.Args) == 0:
				order = "asc"
			case m.Name == "desc" && len(m.Args) == 0:
				// nulls stays "last" (A15): indexConfig.nulls defaults to
				// "last" (pg-core/columns/common.js:74-77).
				order = "desc"
			case m.Name == "nullsFirst" && len(m.Args) == 0:
				nl = "first"
			case m.Name == "nullsLast" && len(m.Args) == 0:
				nl = "last"
			case m.Name == "op" && len(m.Args) == 1:
				s, ok := literalString(m.Args[0])
				if !ok {
					t.extraUnread(line, "%s: .op() is not a string literal", form)
					return
				}
				opclass = s
			default:
				t.extraUnread(line, "%s: column modifier .%s() is not read", form, m.Name)
				return
			}
		}
		cols[i] = c.rec.Name
		orders[i], nulls[i], opclasses[i] = order, nl, opclass
		plain = append(plain, c)
	}
	if name == nil {
		if hasExpr || d != "postgresql" {
			// drizzle-kit exits on an unnamed expression index
			// (bin.cjs:17128-17140); sqlite and mysql require a name.
			t.extraUnread(line, "%s without a name is not read here", form)
			return
		}
		// indexName (bin.cjs:16919): <table>_<cased columns>_index.
		if g.c.Casing != "" {
			g.tag("DR-K3")
		}
		n := t.rec.Name + "_" + strings.Join(casedNames(plain), "_") + "_index"
		name = &n
	}
	entry := map[string]any{"name": t.identifierName(*name), "columns": cols, "unique": unique}
	if hasExpr {
		entry["expressions"] = exprs
	}
	partial, where := false, any(nil)
	for _, l := range links[1:] {
		switch {
		case l.Name == "where" && len(l.Args) == 1 && d != "mysql":
			partial = true
			where = nil
			if text, ok := t.renderSQL(param, l.Args[0], true); ok {
				where = text
			}
		case l.Name == "concurrently" && len(l.Args) == 0 && d == "postgresql":
			// How the index is built, not what it is: no fact.
		case l.Name == "with" && len(l.Args) == 1 && d == "postgresql":
			entry["storage_parameters"] = nil
		default:
			t.extraUnread(line, "%s: .%s() is not read", form, l.Name)
			return
		}
	}
	if d == "postgresql" {
		entry["method"] = method
		entry["orders"], entry["nulls"], entry["opclasses"] = orders, nulls, opclasses
		if _, ok := entry["storage_parameters"]; !ok {
			entry["storage_parameters"] = map[string]any{}
		}
	}
	if d != "mysql" {
		entry["partial"], entry["where"] = partial, where
	}
	g.tag("DR-X1")
	g.tag("DR-X1/" + form)
	t.rec.Indexes = append(t.rec.Indexes, entry)
	t.indexNames = append(t.indexNames, entry["name"].(string))
	if unique && !partial && !hasExpr {
		t.uniqueIndexes = append(t.uniqueIndexes, plain)
	}
	if len(plain) == 1 && !hasExpr {
		switch {
		case !unique:
			plain[0].indexed = true
		case partial:
			plain[0].uniquePartial = true
		default:
			plain[0].uniqueExtra = true
		}
	}
}

func argAt(args []typescript.ModelExpr, i int) typescript.ModelExpr {
	if i < len(args) {
		return args[i]
	}
	return typescript.ModelExpr{Kind: typescript.ModelUnknown}
}

// identifierName is a database identifier as the database keeps it:
// truncated to 63 bytes on postgresql.
func (t *table) identifierName(name string) string {
	if t.g.dialect == "postgresql" {
		return truncateIdentifier(name)
	}
	return name
}

// extraPrimaryKey reads primaryKey({columns: [...], name?}).
func (t *table) extraPrimaryKey(param string, base typescript.ModelExpr, links []link, line int) {
	if len(links) > 0 || len(base.Args) != 1 || base.Args[0].Kind != typescript.ModelObject {
		t.extraUnread(line, "primaryKey: only primaryKey({columns, name?}) is read")
		return
	}
	cfg, ok := configValues(&base.Args[0])
	cols := cfg["columns"]
	if !ok || cols.Kind != typescript.ModelArray || len(cfg) > 2 || len(cfg) == 2 && !hasKey(cfg, "name") {
		t.extraUnread(line, "primaryKey: the config is not {columns: [...], name?}")
		return
	}
	list, why := t.extraColumns(param, cols.Elements)
	if why != "" {
		t.extraUnread(line, "primaryKey: %s", why)
		return
	}
	spec := &pkSpec{cols: list, nameKnown: true, line: line}
	if v, ok := cfg["name"]; ok {
		s, lit := literalString(v)
		spec.name, spec.nameKnown = &s, lit
	}
	if t.pk != nil {
		t.extraUnread(line, "a second primaryKey() element")
		return
	}
	t.g.tag("DR-X1")
	t.g.tag("DR-X1/primaryKey")
	t.pk = spec
}

func hasKey(m map[string]typescript.ModelExpr, k string) bool {
	_, ok := m[k]
	return ok
}

// extraUnique reads unique(name?).on(...) with .nullsNotDistinct().
func (t *table) extraUnique(param string, base typescript.ModelExpr, links []link, line int) {
	name, ok := optionalName(base.Args)
	if !ok || len(base.Args) > 1 || len(links) == 0 || links[0].Name != "on" {
		t.extraUnread(line, "unique: only unique(name?).on(...) is read")
		return
	}
	cols, why := t.extraColumns(param, links[0].Args)
	if why != "" {
		t.extraUnread(line, "unique: %s", why)
		return
	}
	spec := uniqueSpec{cols: cols, name: name, nameKnown: true, line: line}
	for _, l := range links[1:] {
		if l.Name == "nullsNotDistinct" && len(l.Args) == 0 && t.g.dialect == "postgresql" {
			spec.nullsNotDistinct = true
			continue
		}
		t.extraUnread(line, "unique: .%s() is not read", l.Name)
		return
	}
	t.g.tag("DR-X1")
	t.g.tag("DR-X1/unique")
	t.uniques = append(t.uniques, spec)
}

// extraForeignKey reads foreignKey({columns, foreignColumns, name?}) with
// .onDelete(a) and .onUpdate(a).
func (t *table) extraForeignKey(param string, base typescript.ModelExpr, links []link, line int) {
	if len(base.Args) != 1 || base.Args[0].Kind != typescript.ModelObject {
		t.extraUnread(line, "foreignKey: the argument is not an object literal")
		return
	}
	cfg, ok := configValues(&base.Args[0])
	cols, fcols := cfg["columns"], cfg["foreignColumns"]
	if !ok || cols.Kind != typescript.ModelArray || fcols.Kind != typescript.ModelArray || len(cfg) > 3 || len(cfg) == 3 && !hasKey(cfg, "name") {
		t.extraUnread(line, "foreignKey: the config is not {columns, foreignColumns, name?}")
		return
	}
	local, why := t.extraColumns(param, cols.Elements)
	if why != "" {
		t.extraUnread(line, "foreignKey: %s", why)
		return
	}
	spec := &fkSpec{row: "DR-X1", file: t.file, line: line, cols: local, onDelete: "no action", onUpdate: "no action", nameKnown: true}
	if len(fcols.Elements) != len(local) {
		t.extraUnread(line, "foreignKey: columns and foreignColumns differ in length")
		return
	}
	for _, f := range fcols.Elements {
		if f.Kind != typescript.ModelMember || f.Object == nil || f.Object.Kind != typescript.ModelIdentifier {
			t.extraUnread(line, "foreignKey: a foreign column is not <table>.<column>")
			return
		}
		self := param != "" && f.Object.Name == param
		if len(spec.targetKeys) > 0 && (spec.self != self || spec.targetName != f.Object.Name) {
			t.extraUnread(line, "foreignKey: the foreign columns are not of one table")
			return
		}
		spec.self, spec.targetName, spec.targetFile = self, f.Object.Name, t.file
		spec.targetKeys = append(spec.targetKeys, f.Name)
	}
	if v, ok := cfg["name"]; ok {
		s, lit := literalString(v)
		spec.name, spec.nameKnown = &s, lit
	}
	for _, l := range links {
		if (l.Name == "onDelete" || l.Name == "onUpdate") && len(l.Args) == 1 {
			a := action(l.Args[0])
			if l.Name == "onDelete" {
				spec.onDelete = a
			} else {
				spec.onUpdate = a
			}
			continue
		}
		t.extraUnread(line, "foreignKey: .%s() is not read", l.Name)
		return
	}
	t.g.tag("DR-X1")
	t.g.tag("DR-X1/foreignKey")
	t.fks = append(t.fks, spec)
	for _, c := range local {
		c.extraFK = true
	}
}

// referentialActions are the actions drizzle-orm's types allow.
var referentialActions = map[string]bool{"cascade": true, "restrict": true, "no action": true, "set null": true, "set default": true}

// action reads a literal referential action; nil (unknown) otherwise.
// `undefined` keeps "no action" (foreign-keys.js:18-25).
func action(e typescript.ModelExpr) any {
	if e.Kind == typescript.ModelIdentifier && e.Name == "undefined" {
		return "no action"
	}
	if s, ok := literalString(e); ok && referentialActions[s] {
		return s
	}
	return nil
}

// extraCheck reads check(name, sql`...`).
func (t *table) extraCheck(param string, base typescript.ModelExpr, links []link, line int) {
	if len(links) > 0 || len(base.Args) != 2 {
		t.extraUnread(line, "check: only check(name, sql`...`) is read")
		return
	}
	name, ok := literalString(base.Args[0])
	if !ok || base.Args[1].Kind != typescript.ModelTagged || !t.g.in.isSQLTag(t.file, base.Args[1].Tag) {
		t.extraUnread(line, "check: the name is not a string literal or the condition not an sql template")
		return
	}
	text, _ := t.renderSQL(param, base.Args[1], true)
	t.g.tag("DR-X1")
	t.g.tag("DR-X1/check")
	t.rec.Checks = append(t.rec.Checks, map[string]any{"name": t.identifierName(name), "expression": text})
}

// renderSQL renders an sql template by the rendering rule:
// a substitution is a column of this table (`${t.col}` with t the extra
// config's parameter, or `${table.col}` with the table's own const),
// rendered `"<table>"."<column>"` when qualified (where, check) and
// `"<column>"` inside an index expression; anything else, or a table in a
// schema other than public (drizzle-orm qualifies it), is not rendered.
func (t *table) renderSQL(param string, e typescript.ModelExpr, qualified bool) (any, bool) {
	if e.Kind != typescript.ModelTagged || !t.g.in.isSQLTag(t.file, e.Tag) {
		return nil, false
	}
	if t.g.dialect == "mysql" || t.rec.Schema != nil && *t.rec.Schema != "public" || strings.Contains(t.rec.Name, `"`) {
		if len(e.Substitutions) > 0 {
			return nil, false
		}
	}
	var b strings.Builder
	for i, s := range e.Strings {
		b.WriteString(s)
		if i >= len(e.Substitutions) {
			continue
		}
		sub := e.Substitutions[i]
		if sub.Kind != typescript.ModelMember || sub.Object == nil || sub.Object.Kind != typescript.ModelIdentifier {
			return nil, false
		}
		if !(param != "" && sub.Object.Name == param) && sub.Object.Name != t.decl.Name {
			return nil, false
		}
		var target *column
		for _, c := range t.columns {
			if c.key == sub.Name && !c.rec.Unknown {
				target = c
			}
		}
		if target == nil || strings.Contains(target.rec.Name, `"`) {
			return nil, false
		}
		if qualified {
			b.WriteString(`"` + t.rec.Name + `".`)
		}
		b.WriteString(`"` + target.rec.Name + `"`)
	}
	return b.String(), true
}

// applyExtra turns the extra config's keys into the table's records and
// the column flags its absences read; called from finish once the
// columns and DR-P1 checks are done.
func (t *table) applyExtra() {
	g := t.g
	d := g.dialect
	for _, u := range t.uniques {
		var name any
		if n, ok := t.uniqueName(u); ok {
			name = n
		}
		if u.name == nil && g.c.Casing != "" {
			g.tag("DR-K3")
		}
		cols := casedNames(u.cols)
		switch d {
		case "sqlite":
			t.rec.Indexes = append(t.rec.Indexes, map[string]any{"name": name, "columns": cols, "unique": true})
		case "postgresql":
			t.rec.Uniques = append(t.rec.Uniques, map[string]any{"name": name, "columns": cols, "nulls_not_distinct": u.nullsNotDistinct})
		default:
			t.rec.Uniques = append(t.rec.Uniques, map[string]any{"name": name, "columns": cols})
		}
		if len(u.cols) == 1 {
			u.cols[0].uniqueExtra = true
		}
	}
	if t.pk == nil {
		return
	}
	pk := t.pk
	if t.pkColumns > 0 {
		t.rec.ConstraintsComplete = false
		t.rec.PrimaryKeyOpen = true
		t.reason("D", "constraint_set", pk.line, "both .primaryKey() on a column and a primaryKey() element: the table's primary key is not read")
		t.open = true
		return
	}
	if d == "sqlite" && len(pk.cols) == 1 {
		// A one-column primaryKey() is the column's PRIMARY KEY on sqlite
		// (bin.cjs:18742-18758).
		pk.cols[0].primaryKey = true
		pk.cols[0].pkRow = "DR-X1"
		return
	}
	t.pkColumns = len(pk.cols)
	for _, c := range pk.cols {
		c.primaryKey = true
		c.pkRow = "DR-X1"
		if d != "sqlite" {
			// postgresql: a PRIMARY KEY makes its columns NOT NULL; mysql:
			// drizzle-kit sets notNull (bin.cjs:16137-16139). sqlite keeps
			// them nullable.
			if !c.notNull {
				c.notNull, c.ms.notNullRow = true, "DR-X1"
			}
		}
	}
	t.rec.PrimaryKey = casedNames(pk.cols)
	switch {
	case d != "postgresql":
		// sqlite: an unnamed PRIMARY KEY(...) (A10); mysql: always PRIMARY.
		t.rec.Attributes["primary_key_name"] = nil
	case !pk.nameKnown:
		t.rec.Attributes["primary_key_name"] = nil
		t.reason("DR-X1", "primary_key_name", pk.line, "the primary key name is not a string literal")
	default:
		name := ""
		if pk.name != nil {
			name = *pk.name
		} else {
			// primary-keys.js:34: <table>_<builder names>_pk.
			name = t.rec.Name + "_" + strings.Join(builderNames(pk.cols), "_") + "_pk"
		}
		name, ok := t.casedConstraintName(name, pk.cols, nil)
		if ok {
			t.rec.Attributes["primary_key_name"] = t.identifierName(name)
		} else {
			t.rec.Attributes["primary_key_name"] = nil
			t.reason("DR-X1", "primary_key_name", pk.line, "the primary key name under casing is not computed")
		}
	}
}

// uniqueName is a unique() element's name as the database keeps it: the
// literal, else `<table>_<builder names>_unique` (unique-constraint.js:37,
// the pre-casing names), truncated on postgresql.
func (t *table) uniqueName(u uniqueSpec) (string, bool) {
	if !u.nameKnown {
		return "", false
	}
	if u.name != nil {
		return t.identifierName(*u.name), true
	}
	return t.identifierName(t.rec.Name + "_" + strings.Join(builderNames(u.cols), "_") + "_unique"), true
}

// casedConstraintName applies drizzle-kit's casing pass to a primary or
// foreign key name (bin.cjs:17066-17072, :17105-17115): with a literal
// casing, each column's original name is replaced by its cased name, the
// first occurrence only (String.prototype.replace), local columns then
// referenced ones. ok false when a replacement text holds a `$` (a
// replacement pattern in JavaScript).
func (t *table) casedConstraintName(name string, local, ref []*column) (string, bool) {
	if t.g.c.Casing == "" {
		return name, true
	}
	t.g.tag("DR-K3")
	for _, c := range append(append([]*column{}, local...), ref...) {
		if strings.Contains(c.rec.Name, "$") {
			return "", false
		}
		name = strings.Replace(name, c.builderName, c.rec.Name, 1)
	}
	return name, true
}

// pkRowOr returns the row that made the column a primary key column, def
// when no extra-config element did.
func (c *column) pkRowOr(def string) string {
	if c.pkRow != "" {
		return c.pkRow
	}
	return def
}

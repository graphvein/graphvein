package sequelize

import (
	"fmt"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// This file is the claim pass for models, attributes and options (rows
// SQ-K2, SQ-M1, SQ-M2, SQ-A1, SQ-A2, SQ-O1, SQ-O2 and D): one
// table record per model an instance reaches, built as Sequelize 6.37.8's
// init and sync build the table (lib/model.js init, refreshAttributes,
// _addDefaultAttributes, sync; the dialects' attributeToSQL and
// createTableQuery in ddl.go). The association calls run between a
// model's definition and its columns (assoc.go), and the foreign-key
// lists come last (refs.go).

// Diagnostic codes of the claim pass.
const (
	codeImportFailure = "schema_model_import_failure"
	codeNotClaimed    = "schema_model_not_claimed"
)

// listMarker is the suffix of every claim-pass diagnostic about a model
// that may be a table of the group but is not claimed there; the server
// reads it (internal/server modelListMarker) to mark the group's
// table_list may_be_incomplete.
func listMarker(set string) string {
	return "; the table list of model group " + set + " may be incomplete"
}

// group is the claim pass of one instance.
type group struct {
	d     *discovery
	g     *Instance
	steps *models.Steps
	// dialect is the dialect whose rules apply ("" unknown: only the
	// dialect-independent facts are claimed).
	dialect string
	tables  []*table
	// byModel are the tables of the reached models (nil: not claimed).
	byModel map[*Model]*table
	// allUnknown is set when an association call may add or merge
	// attributes into any model of the group (its holder does not
	// resolve, or discovery found association calls it does not read).
	allUnknown string
	allAny     bool
	// throughs counts the belongsToMany calls per string through, and
	// throughUnknown is set when one has a through that is not read (row
	// SQ-R2 claims a join table only for the one call over it);
	// joinUnknown is set when a join table is not claimed.
	throughs       map[string]int
	throughUnknown bool
	joinUnknown    bool
	// touched records, per attribute, the files of the associations that
	// added or merged it (the order between modules may not be proven).
	touched map[*attr]map[string]bool
	// loading is the model being read; throws are the positions of the
	// definitions and association calls that throw while the models
	// load.
	loading *Model
	throws  []loadPos
	// failed counts the failures reported; reading is the table being
	// read (a refused one is dead).
	failed   int
	lastKind failureKind
	reading  *table
	// dead are the tables of the group sync does not create: refused by a
	// failure (provable) or not claimed because they may not exist (a load
	// throw before them); references to them are refused.
	dead []deadTable
}

// deadTable is a table of the group that sync does not create, or may not.
type deadTable struct {
	name     string
	schema   *string
	provable bool
	why      string
}

func (gr *group) tag(row string) { gr.d.in.tag(row) }

// table is one model being read.
type table struct {
	gr  *group
	m   *Model
	rec *models.Table
	sc  scope
	// modelName is the model's name (define's first argument, init's
	// modelName option or the class name).
	modelName string
	opts      *modelOptions
	attrs     []*attr
	// underscored is the model's underscored option.
	underscored bool
	// hasSchema is set when the table has a schema option; failed when a
	// group-level failure refuses it.
	hasSchema bool
	failed    bool
	// indexed and uniqueIdx record the single-column indexes per column
	// name (non-unique; unique and not partial).
	indexed, uniqueIdx map[string]bool
	indexesKnown       bool
	hasIndexes         bool
	// unknownEntries is set when the attributes object holds an entry
	// that is not read (a spread, a method, a computed key).
	unknownEntries bool
	// cols are the read columns' facts; unknownCols the columns that are
	// not read (the cyclic rule cannot tell whether their alter fails).
	cols        []*colFacts
	unknownCols []*colFacts
	// removed are the attribute keys a later removeAttribute("<key>")
	// removes; mutated says why every column is unknown (another
	// mutation after init).
	removed map[string]bool
	mutated string
	// assocUnknown says why an association whose effect is not read may
	// add or merge attributes here (finish).
	assocUnknown string
	assocAny     bool
	// join is the belongsToMany call that defines a join table (row
	// SQ-R2); nil for a model's table.
	join *Association
	// throughKeys are the attribute keys belongsToMany calls assign in
	// this model as their through (key -> call site); throughAll is set
	// when the keys of such a call are not known.
	throughKeys map[string]string
	throughAll  string
}

func (t *table) tag(row string) { t.gr.tag(row) }

// reason adds a table reason.
func (t *table) reason(row, fact string, line int, format string, a ...any) {
	t.rec.Reasons = append(t.rec.Reasons, models.Reason{Row: row, Fact: models.Fact(fact), File: t.m.File, Line: line, Reason: fmt.Sprintf(format, a...)})
}

// columnsPartial marks the column set (and so the implicit primary key)
// not complete.
func (t *table) columnsPartial(row string, line int, format string, a ...any) {
	t.rec.ColumnsComplete = false
	t.reason(row, "column_set", line, format, a...)
}

func (t *table) constraintsPartial(row string, line int, format string, a ...any) {
	t.rec.ConstraintsComplete = false
	t.reason(row, "constraint_set", line, format, a...)
}

// notClaimed reports a model the pass does not read (row D) or whose
// names are unknown (row SQ-K2): no table.
func (gr *group) notClaimed(m *Model, row, format string, a ...any) {
	gr.tag(row)
	gr.steps.Diag(m.File, "info", codeNotClaimed, fmt.Sprintf("%s:%d: %s: ", m.File, m.Span.StartLine, row)+fmt.Sprintf(format, a...)+"; the model is not claimed"+listMarker(gr.g.SetKey()))
}

// failure reports a provable failure (row SQ-P1) of one kind: Sequelize
// throws while defining the model, or the database rejects its table; no
// claim for it. A failure that throws while the models
// load while a model is being read records that model's position.
func (gr *group) failure(kind failureKind, file string, line int, format string, a ...any) {
	gr.tag("SQ-P1")
	gr.failed++
	gr.lastKind = kind
	if kind.info().load && gr.loading != nil {
		gr.throws = append(gr.throws, modelPos(gr.loading))
	}
	gr.steps.Diag(file, "warning", codeImportFailure, fmt.Sprintf("%s:%d: SQ-P1: ", file, line)+fmt.Sprintf(format, a...)+"; the model is not claimed"+listMarker(gr.g.SetKey()))
}

// read builds the group's tables in Sequelize's own order: every model
// as init or define leaves it (attributes, default attributes, mutations),
// then the reached association calls in execution order, which add or
// merge attributes and define join tables (rows SQ-R1, SQ-R2), then each
// table's columns, indexes and uniques as sync writes them, the group's
// failures (row SQ-P1), and the foreign keys with the cyclic rule (row
// SQ-A3).
func (gr *group) read() {
	gr.byModel = map[*Model]*table{}
	for _, m := range gr.g.Models {
		m := m
		gr.loading, gr.reading = m, nil
		before := gr.failed
		gr.steps.Guard(m.File, "model", "Sequelize model at "+spanLine(m.File, m.Span), func() {
			if t := gr.readModel(m); t != nil {
				gr.byModel[m] = t
				gr.tables = append(gr.tables, t)
			}
		})
		if gr.byModel[m] == nil && gr.failed > before && !gr.lastKind.info().kept && gr.reading != nil && gr.reading.rec != nil {
			gr.markDead(gr.reading, true, "a provable failure refuses it")
		}
		gr.loading, gr.reading = nil, nil
	}
	gr.dropAfterThrows()
	gr.associations()
	gr.dropAfterThrows()
	var built []*table
	for _, t := range gr.tables {
		t := t
		if t.failed {
			continue
		}
		ok := false
		before := gr.failed
		gr.steps.Guard(t.m.File, "model", "Sequelize table "+t.rec.Identity, func() { ok = t.complete() })
		if ok {
			built = append(built, t)
		} else if gr.failed > before && !gr.lastKind.info().kept {
			gr.markDead(t, true, "a provable failure refuses it")
		}
	}
	gr.tables = built
	gr.referenceFailures()
	gr.groupFailures()
	gr.missingTables()
	gr.foreignKeys()
}

// dropAfterThrows withdraws the models that a load-time throw
// stops: loading stops at the first throw, so a definition after
// it never runs, and one not proven to run before it may not. Their
// tables are not claimed (a not-claimed diagnostic with the reason).
// markDead refuses a table: sync does not create it (provable) or may not.
func (gr *group) markDead(t *table, provable bool, why string) {
	t.failed = true
	if t.rec != nil {
		gr.dead = append(gr.dead, deadTable{name: t.rec.Name, schema: t.rec.Schema, provable: provable, why: why})
	}
}

func (gr *group) dropAfterThrows() {
	if len(gr.throws) == 0 {
		return
	}
	for _, m := range gr.g.Models {
		t := gr.byModel[m]
		if t == nil || t.failed {
			continue
		}
		after, unproven := gr.afterThrow(modelPos(m))
		why := after
		if why == "" {
			why = unproven
		}
		if why == "" {
			continue
		}
		gr.markDead(t, after != "", why)
		gr.notClaimed(m, "SQ-P1", "%s", why)
	}
	var kept []*table
	for _, t := range gr.tables {
		if !t.failed {
			kept = append(kept, t)
		}
	}
	gr.tables = kept
}

// loadChecks refuses a model whose init throws on what the attributes and
// options already show (row SQ-P1, load failures read before any
// association runs, so their position orders the rest):
// more than one autoIncrement attribute (lib/model.js:247-258) and an index
// without fields (lib/model.js:602-604).
func (t *table) loadChecks() bool {
	ai := 0
	for _, a := range t.attrs {
		if a.unknown != "" {
			continue
		}
		if set, _ := a.autoIncrement.truthy(); set {
			ai++
		}
	}
	if ai > 1 {
		t.gr.failure(failAutoIncrementTwice, t.m.File, t.m.Span.StartLine, "%s: more than one autoIncrement attribute: Sequelize throws (lib/model.js:247-258)", t.modelName)
		return false
	}
	for _, iv := range t.opts.indexes {
		e := iv.e
		if e.Kind != typescript.ModelObject {
			continue
		}
		plain, fields := true, false
		for i := range e.Entries {
			plain = plain && e.Entries[i].Kind == typescript.ModelProperty
			fields = fields || e.Entries[i].Key == "fields"
		}
		if plain && !fields {
			t.gr.failure(failIndexNoFields, iv.file, e.Span.StartLine, "%s: an index without fields: Sequelize throws (lib/model.js:602-604)", t.modelName)
			return false
		}
	}
	return true
}

// complete builds a table's columns, indexes and unique facts once every
// association has run; false when a provable failure refuses it.
func (t *table) complete() bool {
	if !t.columns() {
		return false
	}
	if !t.indexes() {
		return false
	}
	t.uniqueFacts()
	t.finish()
	return true
}

// readModel reads one model; nil when it is not claimed.
func (gr *group) readModel(m *Model) *table {
	d := gr.d
	t := &table{gr: gr, m: m}
	gr.reading = t
	t.sc = scope{d: d, g: gr.g, file: m.File}
	if m.Factory != nil {
		t.sc.fac = m.Factory
		t.sc.locals = bodyLocals(m.Factory.Expr)
	}
	var attrsObj, optsObj *typescript.ModelExpr
	row := "SQ-M1"
	switch m.Form {
	case "init":
		row = "SQ-M2"
		if why := gr.initForm(m); why != "" {
			gr.notClaimed(m, "D", "%s", why)
			return nil
		}
		if m.Call.Args[0].Kind != typescript.ModelObject {
			gr.notClaimed(m, "D", "the attributes argument of %s.init is not an object literal", m.ClassName)
			return nil
		}
		attrsObj, optsObj = &m.Call.Args[0], &m.Call.Args[1]
		t.modelName = m.ClassName
	default:
		args := m.Call.Args
		name, ok := literalString(firstArg(args))
		switch {
		case !ok:
			gr.notClaimed(m, "D", "the model name of define is not a string literal")
			return nil
		case len(args) < 2 || args[1].Kind != typescript.ModelObject:
			gr.notClaimed(m, "D", "the attributes argument of define(%q, ...) is not an object literal", name)
			return nil
		case len(args) > 3 || len(args) == 3 && args[2].Kind != typescript.ModelObject:
			gr.notClaimed(m, "D", "the options argument of define(%q, ...) is not an object literal", name)
			return nil
		}
		attrsObj = &args[1]
		if len(args) == 3 {
			optsObj = &args[2]
		}
		t.modelName = name
	}
	opts, why := mergeOptions(gr.g.Options.Define, optsObj, gr.g.File, m.File)
	if why != "" {
		gr.notClaimed(m, "D", "%s", why)
		return nil
	}
	t.opts = opts
	if v, ok := opts.get("modelName"); ok && m.Form == "init" {
		if n, ok := literalString(v.e); ok && n != "" {
			t.modelName = n
		} else if truthy, known := jsTruthy(v.e); !known || truthy {
			gr.notClaimed(m, "SQ-K2", "the modelName option is not a string literal, so the table name is not known")
			return nil
		}
	}
	if !asciiOnly(t.modelName) || t.modelName == "" {
		gr.notClaimed(m, "SQ-K2", "the model name %q is empty or not ASCII; the inflection port does not read it", t.modelName)
		return nil
	}
	gr.tag(row)
	name, schema, schemaKnown, why := t.tableName()
	if why != "" {
		gr.notClaimed(m, "SQ-K2", "%s", why)
		return nil
	}
	gr.tag("SQ-K2")
	identityName := t.modelName
	if m.Form == "init" {
		identityName = m.ClassName
	}
	t.rec = &models.Table{
		Identity: "table:" + m.File + "#" + identityName, File: m.File, Name: name, Model: identityName,
		ModelLine: m.Span.StartLine, Span: m.Span, Schema: schema, SchemaKnown: schemaKnown,
		ColumnsComplete: true, ConstraintsComplete: true,
		Attributes: map[string]any{"model_name": t.modelName, "model_form": m.Form},
	}
	t.hasSchema = schema != nil
	if !t.readAttributes(attrsObj) {
		return nil
	}
	if !t.defaultAttributes() {
		return nil
	}
	if !t.loadChecks() {
		return nil
	}
	t.mutations()
	return t
}

func firstArg(args []typescript.ModelExpr) *typescript.ModelExpr {
	if len(args) == 0 {
		return nil
	}
	return &args[0]
}

// bodyLocals are the names a factory body declares.
func bodyLocals(fn *typescript.ModelExpr) map[string]bool {
	out := map[string]bool{}
	for _, s := range fn.Statements {
		switch s.Kind {
		case typescript.StatementDeclaration, typescript.StatementClass:
			out[s.Name] = true
		case typescript.StatementDestructure:
			for _, b := range s.Bindings {
				out[b.Name] = true
			}
		}
	}
	for _, p := range fn.Params {
		out[p] = true
	}
	return out
}

// initForm checks row SQ-M2's form: the receiver is a class of the file
// without decorators whose extends chain ends in Sequelize's Model, with
// exactly one unconditional module-level init statement.
func (gr *group) initForm(m *Model) string {
	if m.Class == nil {
		return fmt.Sprintf("the receiver %s of init is not a class declared in %s", m.ClassName, m.File)
	}
	if m.Class.Decorators > 0 {
		return fmt.Sprintf("the class %s has decorators (sequelize-typescript forms are not read)", m.ClassName)
	}
	if why := gr.modelChain(m.File, m.Class, 0); why != "" {
		return fmt.Sprintf("the class %s: %s", m.ClassName, why)
	}
	n := 0
	for _, s := range gr.d.in.Models[m.File].Statements {
		if s.Kind == typescript.StatementExpression && methodCall(s.Value) == "init" && s.Value.Callee.Object.Kind == typescript.ModelIdentifier && s.Value.Callee.Object.Name == m.ClassName {
			n++
		}
	}
	if n > 1 {
		return fmt.Sprintf("%s.init is called %d times at module level", m.ClassName, n)
	}
	for _, c := range clientCalls(gr.d.in.TypeScript[m.File]) {
		recv := c.Expression.Object.Object
		if c.Expression.Object.Name == "init" && recv != nil && recv.Kind == "identifier" && recv.Name == m.ClassName && c.OwnerName != "" {
			return fmt.Sprintf("%s.init is also called inside a function (line %d)", m.ClassName, c.Span.StartLine)
		}
	}
	return ""
}

// modelChain checks that a class's extends chain ends in Sequelize's
// Model (a class of the repository is followed through its own extends).
func (gr *group) modelChain(file string, c *typescript.ModelStatement, depth int) string {
	if depth > maxDepth {
		return "the class chain is too long"
	}
	if c.Extends == nil {
		return "the class chain does not end in Sequelize's Model"
	}
	sc := scope{d: gr.d, g: gr.g, file: file}
	v := sc.resolve(c.Extends)
	switch {
	case v.External == pkgSequelize && v.Name == "Model":
		return ""
	case v.Class != nil:
		if v.Class.Decorators > 0 {
			return "a class of its chain has decorators"
		}
		return gr.modelChain(v.File, v.Class, depth+1)
	}
	return "its extends chain does not end in Sequelize's Model (" + c.Extends.Kind + ")"
}

// tableName derives the table name and schema (row SQ-K2): the tableName
// option, else the model name when freezeTableName, else
// underscoredIf(pluralize(name), underscored) (lib/model.js:705-709); the
// schema option (the model's, else the instance's) on postgres.
func (t *table) tableName() (string, *string, bool, string) {
	o := t.opts
	under, ok := o.truthy("underscored", false)
	if !ok {
		return "", nil, false, "the underscored option is not a literal"
	}
	t.underscored = under
	name := ""
	if v, has := o.get("tableName"); has {
		s, isString := literalString(v.e)
		truthy, known := jsTruthy(v.e)
		switch {
		case isString && s != "":
			name = s
		case !known || truthy:
			return "", nil, false, "the tableName option is not a string literal"
		}
	}
	if name == "" {
		freeze, ok := o.truthy("freezeTableName", false)
		if !ok {
			return "", nil, false, "the freezeTableName option is not a literal"
		}
		if freeze {
			name = t.modelName
		} else {
			p, ok := pluralize(t.modelName)
			if !ok {
				return "", nil, false, "the model name is not ASCII"
			}
			name = p
			if under {
				if name, ok = underscore(p); !ok {
					return "", nil, false, "the model name is not ASCII"
				}
			}
		}
	}
	var schema *string
	if v, has := o.get("schema"); has {
		s, isString := literalString(v.e)
		truthy, known := jsTruthy(v.e)
		switch {
		case isString && s != "":
			schema = &s
		case !known || truthy:
			return "", nil, false, "the schema option is not a string literal"
		}
	} else if gs := t.gr.g.Options; gs.Schema != "" {
		s := gs.Schema
		schema = &s
	} else if !gs.SchemaKnown {
		return "", nil, false, "the instance's schema option is not a string literal"
	}
	if schema != nil && t.gr.dialect != "postgres" {
		return "", nil, false, "a schema option on " + dialectName(t.gr.dialect) + " changes the table's name (row SQ-K2 reads schema on postgres only)"
	}
	if t.gr.dialect == "postgres" && !pgIdentifierOK(name) {
		return "", nil, false, fmt.Sprintf("the table name %q is longer than 63 bytes (postgres truncates it)", name)
	}
	return name, schema, true, ""
}

func dialectName(d string) string {
	if d == "" {
		return "an unknown dialect"
	}
	return d
}

// readAttributes reads the attributes object in order: a key written
// twice keeps its first position and its last value (JavaScript object
// literal semantics). A spread or an unread entry makes the column set
// partial and the default attributes unknown.
func (t *table) readAttributes(obj *typescript.ModelExpr) bool {
	index := map[string]int{}
	for i := range obj.Entries {
		en := &obj.Entries[i]
		switch en.Kind {
		case typescript.ModelProperty, typescript.ModelShorthand:
		default:
			t.tag("D")
			t.columnsPartial("D", en.Span.StartLine, "an entry of the attributes object is not read (a spread, a method or a computed key); the columns it may add or replace are unknown")
			t.rec.PrimaryKeyOpen = true
			t.unknownEntries = true
			continue
		}
		a := t.sc.readAttribute(en.Key, t.m.File, en.Span.StartLine, &en.Value)
		if j, dup := index[en.Key]; dup {
			t.attrs[j] = a
			continue
		}
		index[en.Key] = len(t.attrs)
		t.attrs = append(t.attrs, a)
	}
	for _, a := range t.attrs {
		if a.unknownRow == "SQ-P1" {
			t.gr.failure(failNoType, a.file, a.line, "%s.%s: %s", t.modelName, a.key, a.unknown)
			return false
		}
		if a.typ.key == "ENUM" && a.unknown == "" {
			if a.typ.valuesGiven && a.typ.valuesKnown && len(a.typ.values) == 0 || !a.typ.valuesGiven && !a.valuesGiven {
				t.gr.failure(failEnumNoValues, a.file, a.line, "%s.%s: an ENUM without values: Sequelize throws \"Values for ENUM have not been defined\" (lib/sequelize.js:592-594)", t.modelName, a.key)
				return false
			}
			if a.nonString && t.gr.dialect == "postgres" {
				t.gr.failure(failEnumLabel, a.file, a.line, "%s.%s: an ENUM label that is not a string literal is written unquoted into CREATE TYPE on postgres", t.modelName, a.key)
				return false
			}
		}
		t.nameAttribute(a)
	}
	return true
}

// nameAttribute sets the column name (row SQ-K2).
func (t *table) nameAttribute(a *attr) {
	if a.field != "" || a.nameWhy != "" {
		return
	}
	a.field = a.key
	if t.underscored {
		f, ok := underscore(a.key)
		if !ok {
			a.nameWhy = "the attribute key is not ASCII"
			a.field = a.key
			return
		}
		a.field = f
	}
}

// defaultAttributes adds the implicit id and the timestamp and version
// attributes (row SQ-O1, lib/model.js:188-239): the id first when no
// attribute is a primary key (an `id` attribute without primaryKey makes
// Sequelize throw, row SQ-P1), the others after the user attributes,
// each only when no user attribute has its key.
func (t *table) defaultAttributes() bool {
	o := t.opts
	line := t.m.Span.StartLine
	anyPK, pkKnown := false, !t.unknownEntries
	for _, a := range t.attrs {
		if a.unknown != "" && !a.objectRead {
			// The attribute's keys are not all read: it may be a primary
			// key.
			pkKnown = false
			continue
		}
		set, known := a.primaryKey.truthy()
		anyPK = anyPK || set
		pkKnown = pkKnown && known
	}
	var head []*attr
	switch {
	case anyPK:
	case !pkKnown:
		t.columnsPartial("SQ-O1", line, "an attribute that is not read may be a primary key, so whether Sequelize adds the implicit id is unknown")
		t.rec.PrimaryKeyOpen = true
	default:
		for _, a := range t.attrs {
			if a.key == "id" {
				t.gr.failure(failIDNoPK, a.file, a.line, "%s: an id attribute without primaryKey when no attribute is a primary key: Sequelize throws (lib/model.js:191-193)", t.modelName)
				return false
			}
		}
		head = append(head, &attr{key: "id", file: t.m.File, line: line, typ: typeSpec{key: "INTEGER", form: "INTEGER"}, allowNull: triFalse, primaryKey: triTrue, autoIncrement: triTrue, defaultAttr: "id"})
	}
	var tail []*attr
	stamps, ok := o.truthy("timestamps", true)
	if !ok {
		t.columnsPartial("SQ-O1", line, "the timestamps option is not a literal; the timestamp columns are unknown")
	} else if stamps {
		paranoid, pok := o.truthy("paranoid", false)
		for _, k := range []string{"createdAt", "updatedAt", "deletedAt"} {
			if k == "deletedAt" {
				if !pok {
					t.columnsPartial("SQ-O1", line, "the paranoid option is not a literal; deletedAt is unknown")
					continue
				}
				if !paranoid {
					continue
				}
			}
			name, on, why := timestampName(o, k)
			if why != "" {
				if strings.Contains(why, "throws") {
					t.gr.failure(failTimestampName, t.m.File, line, "%s: %s", t.modelName, why)
					return false
				}
				t.columnsPartial("SQ-O1", line, "%s", why)
				continue
			}
			if on {
				a := &attr{key: name, file: t.m.File, line: line, typ: typeSpec{key: "DATE", form: "DATE"}, defaultAttr: k}
				if k != "deletedAt" {
					a.allowNull = triFalse
				}
				tail = append(tail, a)
			}
		}
	}
	if v, has := o.get("version"); has {
		on, known := jsTruthy(v.e)
		name := "version"
		if s, isString := literalString(v.e); isString && s != "" {
			name = s
		}
		switch {
		case !known:
			t.columnsPartial("SQ-O1", line, "the version option is not a literal; the version column is unknown")
		case on:
			tail = append(tail, &attr{key: name, file: t.m.File, line: line, typ: typeSpec{key: "INTEGER", form: "INTEGER"}, allowNull: triFalse, def: defaultSpec{kind: defNumber, value: "0"}, defaultAttr: "version"})
		}
	}
	user := map[string]bool{}
	for _, a := range t.attrs {
		user[a.key] = true
	}
	all := append([]*attr{}, head...)
	all = append(all, t.attrs...)
	for _, a := range tail {
		if user[a.key] {
			continue
		}
		if t.unknownEntries {
			a.unknown, a.unknownRow = "an unread entry of the attributes object may define this attribute", "SQ-O1"
		}
		all = append(all, a)
	}
	for _, a := range head {
		if t.unknownEntries {
			a.unknown, a.unknownRow = "an unread entry of the attributes object may define a primary key", "SQ-O1"
		}
	}
	for _, a := range all {
		if a.defaultAttr != "" {
			t.tag("SQ-O1")
			t.nameAttribute(a)
		}
	}
	t.attrs = all
	return true
}

// timestampName reads the createdAt, updatedAt or deletedAt option: a
// string names the attribute, false turns it off, true or absent keeps the
// default name (lib/model.js:741-761).
func timestampName(o *modelOptions, key string) (string, bool, string) {
	v, has := o.get(key)
	if !has {
		return key, true, ""
	}
	switch v.e.Kind {
	case typescript.ModelBoolean:
		return key, v.e.Value == "true", ""
	case typescript.ModelString, typescript.ModelTemplate:
		s, ok := literalString(v.e)
		if ok && s == "" {
			return "", false, "the " + key + " option is an empty string: Sequelize throws (lib/model.js:747-749)"
		}
		if ok {
			if !asciiOnly(s) {
				return "", false, "the " + key + " option is not ASCII"
			}
			return s, true, ""
		}
	case typescript.ModelNull, typescript.ModelNumber:
		return "", false, "the " + key + " option is not a string or a boolean: Sequelize throws (lib/model.js:743-745)"
	}
	return "", false, "the " + key + " option is not a literal; its column is unknown"
}

// mutations finds statements that change the model's attributes after
// init: removeAttribute, refreshAttributes and assignments
// to rawAttributes or tableAttributes of the model's class or binding in
// its file (or its factory body). The column set and primary key are then
// not known; `removeAttribute("<key>")` makes that attribute's column
// unknown, any other mutation every column.
func (t *table) mutations() {
	name := t.m.Binding
	if t.m.Form == "init" {
		name = t.m.ClassName
	}
	if name == "" {
		return
	}
	t.removed = map[string]bool{}
	mark := func(line int, what string) {
		t.tag(t.formRow())
		t.columnsPartial(t.formRow(), line, "%s after the model is defined changes its attributes; the column set and primary key are not read", what)
		t.rec.PrimaryKeyOpen = true
	}
	for _, c := range clientCalls(t.gr.d.in.TypeScript[t.m.File]) {
		recv := c.Expression.Object.Object
		method := c.Expression.Object.Name
		if recv == nil || recv.Kind != "identifier" || recv.Name != name {
			continue
		}
		switch method {
		case "removeAttribute":
			mark(c.Span.StartLine, name+".removeAttribute(...)")
			if args := c.Expression.Args; len(args) == 1 && args[0].Kind == "literal" && !strings.ContainsAny(args[0].Name, "\\'\"`") {
				t.removed[args[0].Name] = true
			} else if t.mutated == "" {
				t.mutated = fmt.Sprintf("%s.removeAttribute at line %d with an argument that is not a string literal", name, c.Span.StartLine)
			}
		case "refreshAttributes":
			mark(c.Span.StartLine, name+".refreshAttributes()")
			if t.mutated == "" {
				t.mutated = fmt.Sprintf("%s.refreshAttributes() at line %d", name, c.Span.StartLine)
			}
		}
	}
	check := func(list []typescript.ModelStatement) {
		for i := range list {
			s := &list[i]
			if s.Kind != typescript.StatementExpression || s.Value == nil || s.Value.Kind != typescript.ModelAssign {
				continue
			}
			p := memberPath(s.Value.Left)
			if strings.HasPrefix(p, name+".rawAttributes") || strings.HasPrefix(p, name+".tableAttributes") {
				mark(s.Span.StartLine, "an assignment to "+p)
				if t.mutated == "" {
					t.mutated = fmt.Sprintf("an assignment to %s at line %d", p, s.Span.StartLine)
				}
			}
		}
	}
	if m := t.gr.d.in.Models[t.m.File]; m != nil {
		check(m.Statements)
	}
	if t.m.Factory != nil {
		check(t.m.Factory.Expr.Statements)
	}
}

func (t *table) formRow() string {
	if t.m.Form == "init" {
		return "SQ-M2"
	}
	return "SQ-M1"
}

// finish marks what an association call that is not read may change
// (row SQ-R1's refusal). With literal options whose foreignKey is a
// string or {name, allowNull, field}, Utils.mergeDefaults can only add
// an attribute and fill allowNull, references and the actions into an
// attribute that lacks them: the column set, the nullability of a column
// whose allowNull is absent and every foreign-key list are unknown. With
// options that are not read, any key of the foreignKey object may be
// filled in, so every column is unknown.
func (t *table) finish() {
	why, any := t.assocUnknown, t.assocAny
	if why == "" {
		why, any = t.gr.allUnknown, t.gr.allAny
	}
	if why == "" {
		return
	}
	line := t.m.Span.StartLine
	t.columnsPartial("SQ-R1", line, "association attributes are not read (%s)", why)
	t.rec.PrimaryKeyOpen = t.rec.PrimaryKeyOpen || any
	for _, c := range t.cols {
		if c.rec.Unknown {
			continue
		}
		if any {
			c.unknownColumn(t, "SQ-R1", "an association whose options are not read may merge any attribute key into this attribute (%s)", why)
			continue
		}
		if _, ok := c.rec.Claims["nullable"]; ok && c.a.allowNull == triAbsent && !c.pk && !c.serial {
			delete(c.rec.Claims, "nullable")
			c.rec.Attributes["nullable"] = nil
			c.reason("SQ-R1", "nullable", "an association that is not read may merge allowNull into this attribute (%s)", why)
		}
		c.fkUnknown("SQ-R1", "an association that is not read may merge references into this attribute (%s)", why)
	}
}

// spanOf returns a one-line span.
func spanOf(line int) contract.Span { return contract.Span{StartLine: line, EndLine: line} }

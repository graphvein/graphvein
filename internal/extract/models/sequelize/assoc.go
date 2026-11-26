package sequelize

import (
	"fmt"
	"sort"

	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// This file runs the reached association calls (rows SQ-R1 and SQ-R2) in
// execution order, as Sequelize 6.37.8 does when the call runs
// (lib/associations/mixin.js: the association is built and its
// _injectAttributes merges the foreign-key attribute at once):
// belongsTo, hasOne and hasMany build one foreign-key attribute on the
// holder (belongs-to.js:70-85, has-one.js:73-88, has-many.js:96-112) and
// merge it with Utils.mergeDefaults (lib/utils.js:54-63: every value the
// attribute already has is kept, the first association wins);
// a belongsToMany with a string `through` defines a join table
// (belongs-to-many.js:95-105, :186-247).

// assocOptions are an association call's literal options.
type assocOptions struct {
	as, foreignKey, targetKey, sourceKey string
	// fkObject says the foreignKey option is an object ({name, allowNull,
	// field}); fkAllowNull and fkField are its keys.
	fkObject    bool
	fkAllowNull tri
	fkField     string
	onDelete    action
	onUpdate    action
	constraints tri
	// through, foreignKey and otherKey of a belongsToMany; throughExpr is
	// a model expression `through: X` or `through: {model: X}`.
	through     string
	throughExpr *typescript.ModelExpr
	otherKey    string
	// why says why the options are not read; any says an unread value
	// may set any attribute key (a foreignKey object key or an option
	// with effects that are not read).
	why string
	any bool
}

// assocKeys are the option keys row SQ-R1 reads per method (scope has no
// table fact; belongsTo never reads sourceKey and hasOne and hasMany
// never read targetKey, so the key a method does not read has none).
var assocKeys = map[string]map[string]bool{
	"belongsTo": {"foreignKey": true, "as": true, "targetKey": true, "sourceKey": true, "onDelete": true, "onUpdate": true, "constraints": true, "scope": true},
	"hasOne":    {"foreignKey": true, "as": true, "targetKey": true, "sourceKey": true, "onDelete": true, "onUpdate": true, "constraints": true, "scope": true},
	"hasMany":   {"foreignKey": true, "as": true, "targetKey": true, "sourceKey": true, "onDelete": true, "onUpdate": true, "constraints": true, "scope": true},
	// belongsToMany: row SQ-R2's form, plus `as` (needed only for a
	// self-association's key name, which the row refuses).
	"belongsToMany": {"through": true, "foreignKey": true, "otherKey": true, "as": true},
}

// readAssocOptions reads the options argument of an association call.
func readAssocOptions(method string, e *typescript.ModelExpr) assocOptions {
	o := assocOptions{}
	if e == nil {
		return o
	}
	if e.Kind != typescript.ModelObject {
		return assocOptions{why: "the options argument is not an object literal", any: true}
	}
	str := func(key string, v *typescript.ModelExpr) (string, bool) {
		if isUndefined(v) {
			return "", true
		}
		s, ok := literalString(v)
		if !ok {
			o.why = "the " + key + " option is not a string literal"
		}
		return s, ok
	}
	for i := range e.Entries {
		en := &e.Entries[i]
		if en.Kind != typescript.ModelProperty && en.Kind != typescript.ModelShorthand {
			return assocOptions{why: "an options entry is not a plain property (a spread, a method or a computed key)", any: true}
		}
		if !assocKeys[method][en.Key] {
			return assocOptions{why: "the option " + en.Key + " is not read", any: true}
		}
		v := &en.Value
		switch en.Key {
		case "as":
			if v.Kind == typescript.ModelObject {
				o.why = "an as object is not read"
				continue
			}
			o.as, _ = str("as", v)
		case "foreignKey":
			if v.Kind == typescript.ModelObject {
				o.readForeignKeyObject(v)
				continue
			}
			if v.Kind == typescript.ModelBoolean && v.Value == "false" || v.Kind == typescript.ModelNull {
				continue
			}
			o.foreignKey, _ = str("foreignKey", v)
		case "targetKey":
			if method == "belongsTo" {
				o.targetKey, _ = str("targetKey", v)
			}
		case "sourceKey":
			if method != "belongsTo" {
				o.sourceKey, _ = str("sourceKey", v)
			}
		case "otherKey":
			o.otherKey, _ = str("otherKey", v)
		case "onDelete":
			o.onDelete = readAction(v)
		case "onUpdate":
			o.onUpdate = readAction(v)
		case "constraints":
			o.constraints = readFlag(v)
		case "through":
			if s, ok := literalString(v); ok {
				o.through = s
				continue
			}
			o.throughExpr = v
			if v.Kind == typescript.ModelObject {
				o.throughExpr = objectEntry(v, "model")
			}
		}
	}
	return o
}

// readForeignKeyObject reads `foreignKey: {name, allowNull, field}`.
func (o *assocOptions) readForeignKeyObject(v *typescript.ModelExpr) {
	o.fkObject = true
	for i := range v.Entries {
		en := &v.Entries[i]
		if en.Kind != typescript.ModelProperty {
			o.why, o.any = "the foreignKey object holds an entry that is not read", true
			return
		}
		switch en.Key {
		case "name":
			if s, ok := literalString(&en.Value); ok {
				o.foreignKey = s
			} else if !isUndefined(&en.Value) {
				o.why = "the foreignKey name is not a string literal"
			}
		case "allowNull":
			o.fkAllowNull = readFlag(&en.Value)
			if o.fkAllowNull == triUnknown {
				o.why = "the foreignKey allowNull is not a literal"
			}
		case "field":
			s, ok := literalString(&en.Value)
			switch {
			case isUndefined(&en.Value):
			case !ok || s == "":
				o.why = "the foreignKey field is not a non-empty string literal"
			default:
				o.fkField = s
			}
		default:
			o.why, o.any = "the foreignKey object key "+en.Key+" is not read", true
			return
		}
	}
}

// setAllUnknown records that an association may change any model of the
// group.
func (gr *group) setAllUnknown(why string, any bool) {
	if gr.allUnknown == "" {
		gr.allUnknown = why
	}
	gr.allAny = gr.allAny || any
}

// setUnknown records that an association may change this table.
func (t *table) setUnknown(why string, any bool) {
	if t.assocUnknown == "" {
		t.assocUnknown = why
	}
	t.assocAny = t.assocAny || any
}

// associations runs the group's reached association calls in order.
func (gr *group) associations() {
	if len(gr.g.AttributesIncomplete) > 0 {
		gr.setAllUnknown(gr.g.AttributesIncomplete[0], true)
	}
	for _, t := range gr.tables {
		gr.resolveReferences(t)
	}
	gr.throughs = map[string]int{}
	for _, a := range gr.g.Associations {
		if a.Method != "belongsToMany" || len(a.Call.Args) < 2 {
			continue
		}
		o := readAssocOptions(a.Method, &a.Call.Args[1])
		switch {
		case o.through != "":
			gr.throughs[o.through]++
		case o.throughExpr == nil:
			gr.throughUnknown = true
		}
	}
	gr.touched = map[*attr]map[string]bool{}
	for _, a := range gr.g.Associations {
		a := a
		if after, unproven := gr.afterThrow(gr.assocPos(a)); after != "" {
			// It never runs.
			continue
		} else if unproven != "" {
			gr.unprovenAssociation(a, unproven)
			continue
		}
		done := false
		gr.steps.Guard(a.File, "association", "Sequelize association at "+spanLine(a.File, a.Span), func() {
			gr.associate(a)
			done = true
		})
		if !done {
			gr.setAllUnknown(spanLine(a.File, a.Span)+": the association could not be read", true)
		}
	}
	// Without a proven order between modules, an attribute two modules'
	// associations both touch has values that depend on the order.
	if !gr.g.OrderProven {
		for at, files := range gr.touched {
			if len(files) > 1 && at.unknown == "" {
				at.unknown, at.unknownRow = "associations in more than one module add or merge this attribute, and the order between modules is not proven", "SQ-R1"
			}
		}
	}
	for _, t := range gr.tables {
		t.applyThrough()
	}
}

// unprovenAssociation records an association call that may or may not
// run, because a load-time throw is not proven to come after it:
// what it may add to its holder is unknown, as for an association
// whose effect is not read (a join table it may define is not claimed).
func (gr *group) unprovenAssociation(a *Association, why string) {
	where := spanLine(a.File, a.Span) + ": " + a.Method + "(...) may not run: " + why
	var opts *typescript.ModelExpr
	if len(a.Call.Args) == 2 {
		opts = &a.Call.Args[1]
	}
	o := readAssocOptions(a.Method, opts)
	holderModel := a.Target
	if a.Method == "belongsTo" {
		holderModel = a.Source
	}
	holder := gr.claimed(holderModel)
	if a.Method == "belongsToMany" || holder == nil || len(a.Call.Args) > 2 {
		gr.joinUnknown = gr.joinUnknown || a.Method == "belongsToMany"
		gr.setAllUnknown(where, true)
		return
	}
	holder.setUnknown(where, o.any || o.why != "")
}

// claimed returns the table of a model that is claimed and not refused.
func (gr *group) claimed(m *Model) *table {
	if m == nil {
		return nil
	}
	if t := gr.byModel[m]; t != nil && !t.failed {
		return t
	}
	return nil
}

// associate runs one association call.
func (gr *group) associate(a *Association) {
	where := spanLine(a.File, a.Span)
	var opts *typescript.ModelExpr
	switch n := len(a.Call.Args); {
	case n == 2:
		opts = &a.Call.Args[1]
	case n != 1:
		gr.setAllUnknown(where+": an association call with "+fmt.Sprint(n)+" arguments", true)
		return
	}
	o := readAssocOptions(a.Method, opts)
	if a.Method == "belongsToMany" {
		gr.belongsToMany(a, o, where)
		return
	}
	gr.tag("SQ-R1")
	gr.tag("SQ-R1/" + a.Method)
	holderModel, otherModel := a.Target, a.Source
	if a.Method == "belongsTo" {
		holderModel, otherModel = a.Source, a.Target
	}
	if holderModel == nil {
		gr.setAllUnknown(where+": the model "+a.Method+"(...) adds its foreign key to does not resolve", o.any || o.why != "")
		return
	}
	holder := gr.claimed(holderModel)
	if holder == nil {
		return
	}
	other := gr.claimed(otherModel)
	switch {
	case o.why != "":
		holder.setUnknown(where+": "+a.Method+"(...): "+o.why, o.any)
		return
	case other == nil:
		holder.setUnknown(where+": the other model of "+a.Method+"(...) does not resolve to a claimed model", false)
		return
	}
	spec, why := gr.foreignKeySpec(a, o, holder, other)
	if why != "" {
		holder.setUnknown(where+": "+a.Method+"(...): "+why, false)
		return
	}
	if spec.fail != "" {
		gr.markDead(holder, true, spec.fail)
		if spec.failKind.info().load {
			gr.throws = append(gr.throws, gr.assocPos(a))
		}
		gr.failure(spec.failKind, a.File, a.Span.StartLine, "%s: %s", holder.modelName, spec.fail)
		return
	}
	holder.merge(spec, a)
	if at := holder.attrByKey(spec.name); at != nil {
		if gr.touched[at] == nil {
			gr.touched[at] = map[string]bool{}
		}
		gr.touched[at][a.File] = true
	}
}

// fkSpec is the foreign-key attribute an association builds.
type fkSpec struct {
	name string
	// key is the attribute of the other model the key copies its type
	// from; keyField its column (the references key).
	key      *attr
	keyField string
	field    string
	// allowNull is the new attribute's allowNull (true unless the
	// foreignKey object sets it).
	allowNull tri
	// refs is set when the association writes references (constraints
	// are not false and the other model's key is not a member of its
	// composite primary key, helpers.js:9-21); refTable and refSchema
	// name the other model's table as getTableName returns it.
	refs      bool
	refTable  string
	refSchema *string
	// refsWhy says why whether the association writes references is not
	// known (the attribute's other facts are).
	refsWhy string
	// onDelete and onUpdate are the actions written with references.
	onDelete, onUpdate action
	row                string
	// fail is a provable failure (row SQ-P1) of the holder, of kind
	// failKind.
	fail     string
	failKind failureKind
}

// primaryKey returns the model's primaryKeyAttribute (the first
// primary-key attribute in attribute order) and every primary-key
// attribute. first is known (ok) when every attribute before it is read;
// all is complete (allKnown) when every attribute is. An attribute whose
// value is a `new` expression is never a primary key: normalizeAttribute
// wraps a value that is not a plain object as {type: value}
// (lib/sequelize.js:574-577).
func (t *table) primaryKey() (first *attr, all []*attr, allKnown, ok bool) {
	if t.unknownEntries || t.mutated != "" || len(t.removed) > 0 {
		return nil, nil, false, false
	}
	allKnown = true
	for _, a := range t.attrs {
		if a.unknown != "" && !a.objectRead {
			if a.notPlain {
				continue
			}
			if first == nil {
				return nil, nil, false, false
			}
			allKnown = false
			continue
		}
		set, known := a.primaryKey.truthy()
		if !known {
			if first == nil {
				return nil, nil, false, false
			}
			allKnown = false
			continue
		}
		if set {
			if first == nil {
				first = a
			}
			all = append(all, a)
		}
	}
	if first == nil {
		return nil, nil, false, false
	}
	return first, all, allKnown, true
}

// attrByKey returns the attribute with that key.
func (t *table) attrByKey(key string) *attr {
	for _, a := range t.attrs {
		if a.key == key {
			return a
		}
	}
	return nil
}

// singular is the model's options.name.singular: Utils.singularize of the
// model name (lib/model.js:670-675) unless a name option is given.
func (t *table) singular() (string, bool) {
	if _, has := t.opts.get("name"); has {
		return "", false
	}
	return singularize(t.modelName)
}

// foreignKeySpec builds the foreign-key attribute of a belongsTo, hasOne
// or hasMany call (row SQ-R1).
func (gr *group) foreignKeySpec(a *Association, o assocOptions, holder, other *table) (fkSpec, string) {
	spec := fkSpec{allowNull: triTrue, row: "SQ-R1"}
	if o.fkObject && o.fkAllowNull != triAbsent {
		spec.allowNull = o.fkAllowNull
	}
	spec.field = o.fkField
	pk, pks, pksKnown, ok := other.primaryKey()
	if !ok {
		return spec, "the primary key of " + other.modelName + " is not known"
	}
	keyName := pk.key
	switch a.Method {
	case "belongsTo":
		as := o.as
		if as == "" {
			s, ok := other.singular()
			if !ok {
				return spec, "the singular name of " + other.modelName + " is not known"
			}
			as = s
		}
		spec.name = o.foreignKey
		if spec.name == "" {
			n, ok := camelize(as + "_" + pk.key)
			if !ok {
				return spec, "the default foreign-key name is not ASCII"
			}
			spec.name = n
		}
		if o.targetKey != "" {
			if other.attrByKey(o.targetKey) == nil {
				if !other.rec.ColumnsComplete || other.unknownEntries {
					return spec, "the targetKey " + o.targetKey + " is not a known attribute of " + other.modelName
				}
				spec.fail, spec.failKind = fmt.Sprintf("the targetKey %q is not an attribute of %s: Sequelize throws (associations/belongs-to.js:51-53)", o.targetKey, other.modelName), failMissingKey
				return spec, ""
			}
			keyName = o.targetKey
		}
	case "hasOne":
		base := o.as
		if base == "" {
			base = other.modelName
		}
		spec.name = o.foreignKey
		if spec.name == "" {
			s, ok := singularize(base)
			if !ok {
				return spec, "the default foreign-key name is not ASCII"
			}
			n, ok := camelize(s + "_" + pk.key)
			if !ok {
				return spec, "the default foreign-key name is not ASCII"
			}
			spec.name = n
		}
		if o.sourceKey != "" {
			if other.attrByKey(o.sourceKey) == nil {
				if !other.rec.ColumnsComplete || other.unknownEntries {
					return spec, "the sourceKey " + o.sourceKey + " is not a known attribute of " + other.modelName
				}
				spec.fail, spec.failKind = fmt.Sprintf("the sourceKey %q is not an attribute of %s: Sequelize throws (associations/has-one.js:53-55)", o.sourceKey, other.modelName), failMissingKey
				return spec, ""
			}
			keyName = o.sourceKey
		}
	case "hasMany":
		spec.name = o.foreignKey
		if spec.name == "" {
			s, ok := other.singular()
			if !ok {
				return spec, "the singular name of " + other.modelName + " is not known"
			}
			n, ok := camelize(s + "_" + pk.key)
			if !ok {
				return spec, "the default foreign-key name is not ASCII"
			}
			spec.name = n
		}
		if o.sourceKey != "" {
			// has-many.js:70-76 falls back to the primary key silently
			// when the sourceKey is not an attribute: only a known
			// attribute is read.
			if other.attrByKey(o.sourceKey) == nil {
				return spec, "the sourceKey " + o.sourceKey + " is not a known attribute of " + other.modelName
			}
			keyName = o.sourceKey
		}
	}
	key := other.attrByKey(keyName)
	if key == nil || key.nameWhy != "" || key.field == "" {
		return spec, "the key attribute " + keyName + " of " + other.modelName + " or its column name is not known"
	}
	spec.key, spec.keyField = key, key.field
	if !asciiOnly(spec.name) || spec.name == "" {
		return spec, "the foreign-key name is empty or not ASCII"
	}
	// The default onDelete reads the holder's existing attribute, else
	// the new one (belongs-to.js:77-80, has-many.js:103-106,
	// has-one.js:78-81).
	allowNull := spec.allowNull
	if ex := holder.attrByKey(spec.name); ex != nil {
		if ex.unknown != "" && !ex.objectRead {
			return spec, "the existing attribute " + spec.name + " is not read"
		}
		allowNull = ex.allowNull
	}
	switch o.constraints {
	case triUnknown:
		return spec, "the constraints option is not a literal"
	case triFalse:
		if o.onDelete.value != "" || o.onUpdate.value != "" || o.onDelete.why != "" || o.onUpdate.why != "" {
			return spec, "constraints: false with an onDelete or onUpdate (Sequelize still writes references; not a form the oracle compared)"
		}
		return spec, ""
	}
	switch {
	case o.onDelete.why != "":
		return spec, "onDelete: " + o.onDelete.why
	case o.onUpdate.why != "":
		return spec, "onUpdate: " + o.onUpdate.why
	}
	spec.onDelete = o.onDelete
	if spec.onDelete.value == "" {
		nullable, known := allowNull.truthy()
		switch {
		case !known:
			return spec, "the allowNull that decides the default onDelete is not a literal"
		case nullable:
			spec.onDelete = action{present: true, value: "SET NULL"}
		case a.Method == "belongsTo":
			spec.onDelete = action{present: true, value: "NO ACTION"}
		default:
			spec.onDelete = action{present: true, value: "CASCADE"}
		}
	}
	spec.onUpdate = o.onUpdate
	if spec.onUpdate.value == "" {
		spec.onUpdate = action{present: true, value: "CASCADE"}
	}
	// helpers.js:12-13: a member of a composite primary key gets no
	// references.
	member := false
	for _, p := range pks {
		member = member || p.field == spec.keyField
	}
	switch {
	case member && len(pks) > 1:
		return spec, ""
	case member && !pksKnown:
		spec.refsWhy = "an attribute of " + other.modelName + " that is not read may make its primary key composite, which decides whether the key gets references (associations/helpers.js:12-13)"
		return spec, ""
	}
	spec.refs = true
	spec.refTable, spec.refSchema = other.rec.Name, other.rec.Schema
	if gr.dialect == "postgres" && other.rec.Schema == nil && holder.rec.Schema != nil {
		spec.fail, spec.failKind = fmt.Sprintf("%s(%s) from a model in schema %s writes REFERENCES %q.%q, a table the group does not create", a.Method, other.modelName, *holder.rec.Schema, *holder.rec.Schema, other.rec.Name), failCrossSchema
	}
	return spec, ""
}

// merge adds the association's attribute to the holder, or merges it into
// the attribute of that key without replacing any value it has
// (Utils.mergeDefaults).
func (t *table) merge(spec fkSpec, a *Association) {
	line := a.Span.StartLine
	ref := func() *refSpec {
		r := &refSpec{row: spec.row, file: a.File, line: line, key: spec.keyField, keyGiven: true, table: spec.refTable, schema: spec.refSchema, resolved: true}
		return r
	}
	ex := t.attrByKey(spec.name)
	if ex == nil {
		n := &attr{key: spec.name, file: a.File, line: line, typ: spec.key.typ, allowNull: spec.allowNull, field: spec.field, assocRow: spec.row, objectRead: true}
		if spec.key.unknown != "" || spec.key.virtual {
			n.unknown, n.unknownRow = "the key attribute "+spec.key.key+" the foreign key copies its type from is not read", spec.row
		}
		switch {
		case spec.refsWhy != "":
			n.references, n.ref = true, &refSpec{row: spec.row, file: a.File, line: line, why: spec.refsWhy}
		case spec.refs:
			n.references, n.ref, n.refText, n.refKnown = true, ref(), spec.refTable, true
			n.onDelete, n.onUpdate = spec.onDelete, spec.onUpdate
		}
		t.nameAttribute(n)
		t.attrs = append(t.attrs, n)
		return
	}
	if ex.allowNull == triAbsent && spec.allowNull != triAbsent {
		ex.allowNull = spec.allowNull
		ex.nullRow, ex.nullFile, ex.nullLine = spec.row, a.File, line
	}
	if spec.refsWhy != "" {
		switch {
		case !ex.references:
			ex.references, ex.ref = true, &refSpec{row: spec.row, file: a.File, line: line, why: spec.refsWhy}
		case ex.ref != nil && ex.ref.why == "" && (!ex.ref.keyGiven || !ex.onDelete.present || !ex.onUpdate.present):
			ex.ref.why = spec.refsWhy
		}
		return
	}
	if !spec.refs {
		return
	}
	switch {
	case !ex.references:
		ex.references, ex.ref, ex.refText, ex.refKnown = true, ref(), spec.refTable, true
	case ex.ref != nil && ex.ref.why == "" && !ex.ref.keyGiven:
		if ex.ref.schema != nil || spec.refSchema != nil {
			ex.ref.why = "an association merges into a references object of a table with a schema; the merged model is not read"
			break
		}
		ex.ref.key, ex.ref.keyGiven = spec.keyField, true
	}
	if !ex.onDelete.present {
		ex.onDelete = spec.onDelete
	}
	if !ex.onUpdate.present {
		ex.onUpdate = spec.onUpdate
	}
}

// belongsToMany runs a belongsToMany call (row SQ-R2): a string through
// on two different models, the only call over that through, defines the
// join table; a through model gets the two keys Object.assign'd and may
// lose its implicit id (belongs-to-many.js:186-247): its key columns and
// primary key are not read.
func (gr *group) belongsToMany(a *Association, o assocOptions, where string) {
	gr.tag("SQ-R2")
	if o.why != "" && o.through == "" && o.throughExpr == nil {
		gr.setAllUnknown(where+": belongsToMany(...): "+o.why, true)
		return
	}
	src, tgt := gr.claimed(a.Source), gr.claimed(a.Target)
	if o.throughExpr != nil {
		var through *table
		if m := gr.d.modelRef(gr.g, a.File, o.throughExpr); m != nil && a.Context == "module" {
			through = gr.claimed(m)
			if through == nil {
				return
			}
		}
		if through == nil {
			gr.setAllUnknown(where+": the through model of belongsToMany(...) does not resolve", true)
			return
		}
		through.addThroughKeys(gr.throughKeys(a, o, src, tgt), where)
		return
	}
	if o.through == "" {
		gr.setAllUnknown(where+": the through option of belongsToMany(...) is not read", true)
		return
	}
	refuse := func(format string, args ...any) {
		gr.steps.Diag(a.File, "info", codeNotClaimed, fmt.Sprintf("%s: SQ-R2: belongsToMany(...) over %q: ", where, o.through)+fmt.Sprintf(format, args...)+"; the join table is not claimed"+listMarker(gr.g.SetKey()))
		gr.joinUnknown = true
	}
	switch {
	case o.why != "":
		refuse("%s", o.why)
		return
	case src == nil || tgt == nil:
		refuse("a model of the call is not a claimed model of the group")
		return
	case src == tgt:
		refuse("a self-association (Sequelize names its keys from `as` and pairs the calls over one through; not read)")
		return
	case gr.throughs[o.through] > 1 || gr.throughUnknown:
		refuse("another belongsToMany may use the same through (Sequelize pairs the calls and rewrites their keys)")
		return
	case o.as != "":
		refuse("the as option is not part of row SQ-R2's form")
		return
	}
	for _, t := range gr.allTables() {
		if t.modelName == o.through {
			refuse("a model of the group is named %q (Sequelize uses that model as the through)", o.through)
			return
		}
	}
	if gr.dialect == "postgres" && !pgIdentifierOK(o.through) {
		refuse("the table name is longer than 63 bytes")
		return
	}
	join, why, fail := gr.joinTable(a, o, src, tgt)
	switch {
	case fail != "":
		gr.failure(failCrossSchema, a.File, a.Span.StartLine, "belongsToMany(...) over %q: %s", o.through, fail)
		return
	case why != "":
		refuse("%s", why)
		return
	}
	gr.tables = append(gr.tables, join)
}

// throughKeys are the attribute keys a belongsToMany over a through model
// writes (foreignKey, otherKey; belongs-to-many.js:148-176); nil when
// they are not known.
func (gr *group) throughKeys(a *Association, o assocOptions, src, tgt *table) []string {
	if src == nil || tgt == nil || o.why != "" {
		return nil
	}
	fk, ok := o.foreignKey, true
	if fk == "" {
		fk, ok = defaultKey(src, nil)
	}
	if !ok {
		return nil
	}
	other := o.otherKey
	if other == "" {
		var as *string
		if src == tgt {
			if o.as == "" {
				return nil
			}
			s, ok := singularize(o.as)
			if !ok {
				return nil
			}
			as = &s
		}
		if other, ok = defaultKey(tgt, as); !ok {
			return nil
		}
	}
	return []string{fk, other}
}

// defaultKey is camelize(<singular>_<primary key>) of a model, with name
// in place of its singular name when given.
func defaultKey(t *table, name *string) (string, bool) {
	pk, _, _, ok := t.primaryKey()
	if !ok {
		return "", false
	}
	s := ""
	if name != nil {
		s = *name
	} else if s, ok = t.singular(); !ok {
		return "", false
	}
	return camelize(s + "_" + pk.key)
}

// addThroughKeys records the keys a belongsToMany writes into this
// through model (nil keys: not known).
func (t *table) addThroughKeys(keys []string, where string) {
	if keys == nil {
		t.throughAll = where
		return
	}
	if t.throughKeys == nil {
		t.throughKeys = map[string]string{}
	}
	for _, k := range keys {
		if t.throughKeys[k] == "" {
			t.throughKeys[k] = where
		}
	}
}

// applyThrough makes the attributes belongsToMany calls write into this
// through model unknown: the keys (or every attribute when they are not
// known) and the implicit id the call deletes; the column set and
// primary key are then partial, and a unique group over the keys may be
// added.
func (t *table) applyThrough() {
	if t.throughAll == "" && t.throughKeys == nil {
		return
	}
	line := t.m.Span.StartLine
	t.tag("SQ-R2")
	if t.throughAll != "" {
		t.columnsPartial("SQ-R2", line, "%s: belongsToMany(...) uses this model as its through with keys that are not read", t.throughAll)
	} else {
		keys := make([]string, 0, len(t.throughKeys))
		for k := range t.throughKeys {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			where := t.throughKeys[k]
			if at := t.attrByKey(k); at != nil {
				at.unknown, at.unknownRow = where+": belongsToMany(...) assigns this attribute as its through key", "SQ-R2"
			} else {
				t.columnsPartial("SQ-R2", line, "%s: belongsToMany(...) adds the through key %s", where, k)
			}
		}
	}
	for _, at := range t.attrs {
		if t.throughAll != "" && at.unknown == "" {
			at.unknown, at.unknownRow = t.throughAll+": belongsToMany(...) may assign this attribute as its through key", "SQ-R2"
		}
		if at.defaultAttr == "id" && at.unknown == "" {
			at.unknown, at.unknownRow = "belongsToMany(...) deletes the implicit id of its through model and makes the keys the primary key", "SQ-R2"
		}
	}
	t.rec.PrimaryKeyOpen = true
	t.constraintsPartial("SQ-R2", line, "belongsToMany(...) may add a unique group over its through keys")
}

// joinTable builds the join table of a belongsToMany with a string
// through: the source's options (underscored, createdAt/updatedAt names,
// version, schema) and the instance's global timestamps, createdAt and
// updatedAt first, then the two keys, together the primary key, each a
// CASCADE foreign key to its model.
func (gr *group) joinTable(a *Association, o assocOptions, src, tgt *table) (*table, string, string) {
	m := &Model{File: a.File, Form: "belongsToMany", Context: "association", Span: a.Span, Call: a.Call}
	t := &table{gr: gr, m: m, join: a, modelName: o.through, underscored: src.underscored, removed: map[string]bool{}}
	t.sc = scope{d: gr.d, g: gr.g, file: a.File}
	t.opts = &modelOptions{vals: map[string]optValue{}}
	var schema *string
	if src.rec.Schema != nil {
		s := *src.rec.Schema
		schema = &s
	}
	if gr.dialect == "postgres" && tgt.rec.Schema == nil && schema != nil {
		return nil, "", fmt.Sprintf("the join table is in schema %s (the source's) and its REFERENCES to %s is resolved there", *schema, tgt.rec.Name)
	}
	t.rec = &models.Table{
		Identity: "table:" + a.File + "#" + o.through, File: a.File, Name: o.through, Model: o.through,
		ModelLine: a.Span.StartLine, Span: a.Span, Schema: schema, SchemaKnown: true,
		ColumnsComplete: true, ConstraintsComplete: true,
		Attributes: map[string]any{"model_name": o.through, "model_form": "belongsToMany"},
	}
	line := a.Span.StartLine
	newAttr := func(key string) *attr {
		return &attr{key: key, file: a.File, line: line, assocRow: "SQ-R2", objectRead: true}
	}
	global, why := mergeOptions(gr.g.Options.Define, nil, gr.g.File, gr.g.File)
	if why != "" {
		return nil, "the global define is not read: " + why, ""
	}
	stamps, ok := global.truthy("timestamps", true)
	if !ok {
		return nil, "the global timestamps option is not a literal", ""
	}
	if stamps {
		for _, k := range []string{"createdAt", "updatedAt"} {
			name, on, why := timestampName(src.opts, k)
			if why != "" {
				return nil, why, ""
			}
			if on {
				at := newAttr(name)
				at.typ, at.allowNull = typeSpec{key: "DATE", form: "DATE"}, triFalse
				t.attrs = append(t.attrs, at)
			}
		}
	}
	if v, has := src.opts.get("version"); has {
		on, known := jsTruthy(v.e)
		if !known || on {
			return nil, "the source's version option adds a version column to the join table (not a form the oracle compared)", ""
		}
	}
	srcPK, _, _, ok := src.primaryKey()
	if !ok {
		return nil, "the primary key of " + src.modelName + " is not known", ""
	}
	tgtPK, _, _, ok := tgt.primaryKey()
	if !ok {
		return nil, "the primary key of " + tgt.modelName + " is not known", ""
	}
	fk := o.foreignKey
	if fk == "" {
		if fk, ok = defaultKey(src, nil); !ok {
			return nil, "the default foreignKey is not known", ""
		}
	}
	other := o.otherKey
	if other == "" {
		if other, ok = defaultKey(tgt, nil); !ok {
			return nil, "the default otherKey is not known", ""
		}
	}
	if fk == other || !asciiOnly(fk) || !asciiOnly(other) {
		return nil, "the two keys have one name or are not ASCII", ""
	}
	for _, k := range []struct {
		name   string
		pk     *attr
		target *table
	}{{fk, srcPK, src}, {other, tgtPK, tgt}} {
		if k.pk.unknown != "" || k.pk.virtual || k.pk.field == "" || k.pk.nameWhy != "" {
			return nil, "the primary key of " + k.target.modelName + " is not read", ""
		}
		at := newAttr(k.name)
		at.typ, at.primaryKey = k.pk.typ, triTrue
		at.references, at.refText, at.refKnown = true, k.target.rec.Name, true
		at.ref = &refSpec{row: "SQ-R2", file: a.File, line: line, key: k.pk.field, keyGiven: true, table: k.target.rec.Name, schema: k.target.rec.Schema, resolved: true}
		at.onDelete = action{present: true, value: "CASCADE"}
		at.onUpdate = action{present: true, value: "CASCADE"}
		t.attrs = append(t.attrs, at)
	}
	for _, at := range t.attrs {
		t.nameAttribute(at)
	}
	gr.tag("SQ-R2")
	return t, "", ""
}

package sequelize

import (
	"fmt"

	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// This file reads one attribute of a model's attributes object (rows
// SQ-A1 and SQ-A2) into an attr: its type, the keys with a table fact and
// the default. Sequelize's normalizeAttribute (lib/sequelize.js:574-597)
// turns a value that is not a plain object into {type: value}; the
// shorthand `key: DataTypes.X` is read the same way.

// tri is a literal flag: absent, true, false, or not a literal.
type tri int

const (
	triAbsent tri = iota
	triTrue
	triFalse
	triUnknown
)

// truthy says whether a known flag is set (Sequelize tests these keys
// for truthiness); known is false for triUnknown.
func (t tri) truthy() (set, known bool) {
	switch t {
	case triTrue:
		return true, true
	case triUnknown:
		return false, false
	}
	return false, true
}

// readFlag reads a boolean literal; `undefined` is absent.
func readFlag(e *typescript.ModelExpr) tri {
	switch {
	case e.Kind == typescript.ModelBoolean && e.Value == "true":
		return triTrue
	case e.Kind == typescript.ModelBoolean:
		return triFalse
	case isUndefined(e):
		return triAbsent
	}
	return triUnknown
}

func isUndefined(e *typescript.ModelExpr) bool {
	return e != nil && e.Kind == typescript.ModelIdentifier && e.Name == "undefined"
}

// Default kinds of an attribute's defaultValue (row SQ-A2).
const (
	defAbsent  = "absent"
	defNull    = "null"
	defString  = "string"
	defNumber  = "number"
	defBoolean = "boolean"
	defJSON    = "json"
	defLiteral = "literal"
	defFn      = "fn"
	defClient  = "client"
)

// defaultSpec is an attribute's defaultValue as written.
type defaultSpec struct {
	kind string
	// value is the string value, the number's JavaScript text, the
	// boolean ("true"/"false") or the JSON.stringify text (json); for
	// literal and fn the SQL text Sequelize writes (fn's name and
	// arguments), used only to check it for the markers Sequelize's DDL
	// rewriting matches.
	value string
	// why says why the default is not read (kind "").
	why string
}

// Unique forms of an attribute's unique key (row SQ-A2).
const (
	uniqueNone  = ""
	uniqueTrue  = "true"
	uniqueGroup = "group"
)

type uniqueSpec struct {
	kind string
	// name is the group name (`unique: "g"` or `unique: {name: "g"}`).
	name    string
	unknown string
}

// attr is one attribute as read.
type attr struct {
	key, file string
	line      int
	// field is the column name: the literal `field`, else the key, through
	// underscore when the model is underscored (lib/model.js:838-840).
	field string
	// nameWhy says why the column name is not known.
	nameWhy string
	// unknown says why the whole column is unknown (row unknownRow); a
	// column whose type is not read is unknown.
	unknown, unknownRow string
	virtual             bool
	typ                 typeSpec
	allowNull           tri
	primaryKey          tri
	autoIncrement       tri
	unique              uniqueSpec
	def                 defaultSpec
	// index is the attribute's `index` key.
	index tri
	// references is set when the attribute has a `references` key (its
	// own, or one an association merged in; row SQ-A3); refText is the
	// referenced table's name when it is known (the rendered REFERENCES
	// clause is checked for the markers Sequelize's DDL rewriting
	// matches).
	references bool
	refText    string
	refKnown   bool
	// ref is the references object as read (refs.go); nil without one.
	ref *refSpec
	// onDelete and onUpdate are the attribute's own action keys (written
	// by the user or merged in by an association).
	onDelete, onUpdate action
	// assocRow is SQ-R1 or SQ-R2 for an attribute an association added:
	// its facts are that row's claims, at the association's site.
	assocRow string
	// nullRow is set when an association merged allowNull into an
	// attribute that had none (Utils.mergeDefaults): the nullability is
	// that row's claim.
	nullRow  string
	nullFile string
	nullLine int
	// comment is the `comment` key; commentText its literal value.
	comment      bool
	commentText  string
	commentKnown bool
	// values is the attribute's `values` key (it replaces an ENUM's
	// labels, lib/sequelize.js:586-591).
	values      []string
	valuesKnown bool
	valuesGiven bool
	// nonString is set when ENUM labels hold a literal that is not a
	// string.
	nonString bool
	// notPlain is set when the attribute's value is a `new` expression:
	// never a plain object, so normalizeAttribute makes it the type and
	// the attribute has no other key (no primary key).
	notPlain bool
	// newVirtual is set for `new DataTypes.VIRTUAL(...)`: its column is
	// not read (row SQ-A1 refuses `new` forms), but it has none, so the
	// cyclic alter pass cannot fail on it.
	newVirtual bool
	// objectRead is set when the attribute is an object literal whose
	// keys were all read (its type may still be unknown).
	objectRead bool
	// helper is set when the value is an attribute helper's member (row
	// SQ-A1, form "helper member"), read as substituted.
	helper bool
	// defaultAttr names the default attribute (row SQ-O1): "id",
	// "createdAt", "updatedAt", "deletedAt" or "version"; "" for a user
	// attribute.
	defaultAttr string
}

// row is the contract row of a fact of this attribute: the association's
// row for an attribute an association added, SQ-O1 for a default
// attribute, else base.
func (a *attr) row(base string) string {
	if a.assocRow != "" {
		return a.assocRow
	}
	if a.defaultAttr != "" {
		return "SQ-O1"
	}
	return base
}

// noFactKeys are attribute keys with no table fact (row SQ-A2).
var noFactKeys = map[string]bool{"validate": true, "get": true, "set": true}

// readAttribute reads one attributes-object entry key: value. A value
// that calls an attribute helper (row SQ-A1, form "helper member",
// helpers.go) is read as the helper's substituted member, or is unknown
// when the helper is not of the form.
func (s scope) readAttribute(key, file string, line int, v *typescript.ModelExpr) *attr {
	sub, why, ok := s.helperValue(v)
	switch {
	case ok && why != "":
		return &attr{key: key, file: file, line: line, unknown: why, unknownRow: "SQ-A1"}
	case ok:
		s.d.in.tag(helperForm)
		a := s.readAttributeValue(key, file, line, sub)
		a.helper = true
		return a
	}
	return s.readAttributeValue(key, file, line, v)
}

// readAttributeValue reads an attribute value as written (or as a helper
// returns it).
func (s scope) readAttributeValue(key, file string, line int, v *typescript.ModelExpr) *attr {
	a := &attr{key: key, file: file, line: line}
	if v.Kind != typescript.ModelObject {
		// normalizeAttribute: {type: value}.
		a.notPlain = v.Kind == typescript.ModelNew
		a.newVirtual = a.notPlain && v.Callee != nil && v.Callee.Kind == typescript.ModelMember && v.Callee.Name == "VIRTUAL" && s.isTypes(v.Callee.Object)
		t, why := s.readType(v)
		if why != "" {
			a.unknown, a.unknownRow = why, "SQ-A1"
			return a
		}
		a.setType(t)
		if t.key == "ENUM" && v.Kind == typescript.ModelCall {
			a.nonString = enumNonString(v.Args)
		}
		return a
	}
	seen := map[string]*typescript.ModelExpr{}
	var order []string
	for i := range v.Entries {
		en := &v.Entries[i]
		switch en.Kind {
		case typescript.ModelProperty, typescript.ModelShorthand:
		case typescript.ModelMethod:
			if noFactKeys[en.Key] {
				continue
			}
			a.unknown, a.unknownRow = fmt.Sprintf("the method %s of the attribute object is not read (row D)", en.Key), "D"
			return a
		default:
			a.unknown, a.unknownRow = fmt.Sprintf("an entry of the attribute object at line %d is not read (a spread or a computed key, row D)", en.Span.StartLine), "D"
			return a
		}
		if seen[en.Key] == nil {
			order = append(order, en.Key)
		}
		seen[en.Key] = &en.Value
	}
	typeExpr := seen["type"]
	if typeExpr == nil || isUndefined(typeExpr) {
		a.unknown, a.unknownRow = "the attribute has no type: Sequelize throws \"Unrecognized datatype\" (lib/model.js:715-717)", "SQ-P1"
		return a
	}
	// An unread type makes the column unknown; the other keys are still
	// read, so the model's primary key (and with it the implicit id) can
	// be known.
	t, typeWhy := s.readType(typeExpr)
	if typeWhy == "" {
		a.setType(t)
		if t.key == "ENUM" && typeExpr.Kind == typescript.ModelCall {
			a.nonString = enumNonString(typeExpr.Args)
		}
	}
	for _, k := range order {
		e := seen[k]
		switch k {
		case "type":
		case "allowNull":
			a.allowNull = readFlag(e)
		case "primaryKey":
			a.primaryKey = readFlag(e)
		case "autoIncrement":
			a.autoIncrement = readFlag(e)
		case "unique":
			a.unique = readUnique(e)
		case "defaultValue":
			a.def = s.readDefault(e)
		case "field":
			if isUndefined(e) {
				continue
			}
			f, ok := literalString(e)
			switch {
			case !ok:
				a.nameWhy = "the field option is not a string literal"
			case f == "":
				a.nameWhy = "the field option is an empty string"
			default:
				a.field = f
			}
		case "values":
			if isUndefined(e) {
				continue
			}
			a.valuesGiven = true
			if e.Kind == typescript.ModelArray {
				a.values, a.valuesKnown = enumValues([]typescript.ModelExpr{*e})
				a.nonString = a.nonString || enumNonString([]typescript.ModelExpr{*e})
			}
		case "index":
			a.index = readFlag(e)
		case "references":
			if isUndefined(e) {
				continue
			}
			a.references = true
			a.ref = readReferences(e, file, line)
			if a.ref.model != nil {
				a.refText, a.refKnown = literalString(a.ref.model)
			}
		case "onDelete":
			a.onDelete = readAction(e)
		case "onUpdate":
			a.onUpdate = readAction(e)
		case "comment":
			a.comment = !isUndefined(e)
			a.commentText, a.commentKnown = literalString(e)
		default:
			if noFactKeys[k] {
				continue
			}
			a.unknown, a.unknownRow = "the attribute key "+k+" is not read (row D)", "D"
			return a
		}
	}
	a.objectRead = true
	if typeWhy != "" {
		a.unknown, a.unknownRow = typeWhy, "SQ-A1"
		return a
	}
	if a.typ.key == "ENUM" && a.valuesGiven {
		// The attribute's values replace the type's labels.
		a.typ.values, a.typ.valuesKnown, a.typ.valuesGiven = a.values, a.valuesKnown, true
	}
	return a
}

func (a *attr) setType(t typeSpec) {
	if t.key == "VIRTUAL" {
		a.virtual = true
	}
	a.typ = t
}

// readUnique reads the unique key: true, false, a group name, or {name}
// (an optional msg beside it has no table fact).
func readUnique(e *typescript.ModelExpr) uniqueSpec {
	switch {
	case isUndefined(e):
		return uniqueSpec{}
	case e.Kind == typescript.ModelBoolean && e.Value == "true":
		return uniqueSpec{kind: uniqueTrue}
	case e.Kind == typescript.ModelBoolean:
		return uniqueSpec{}
	}
	if s, ok := literalString(e); ok {
		if s == "" {
			// An empty string is falsy: no unique key.
			return uniqueSpec{}
		}
		return uniqueSpec{kind: uniqueGroup, name: s}
	}
	if e.Kind == typescript.ModelObject {
		name := ""
		for i := range e.Entries {
			en := &e.Entries[i]
			if en.Kind != typescript.ModelProperty {
				return uniqueSpec{unknown: "the unique object holds an entry that is not read"}
			}
			switch en.Key {
			case "name":
				n, ok := literalString(&en.Value)
				if !ok {
					return uniqueSpec{unknown: "the unique name is not a string literal"}
				}
				name = n
			case "msg":
			default:
				return uniqueSpec{unknown: "the unique object key " + en.Key + " is not read"}
			}
		}
		if name == "" {
			return uniqueSpec{unknown: "a unique object without a name (Sequelize names the group <table>_<attribute>_unique; not compared)"}
		}
		return uniqueSpec{kind: uniqueGroup, name: name}
	}
	return uniqueSpec{unknown: "the unique value is not a literal"}
}

// readDefault reads defaultValue: a literal, `sequelize.literal(...)` /
// `fn(...)`, the client-side NOW, UUIDV4 and UUIDV1 or a function.
func (s scope) readDefault(e *typescript.ModelExpr) defaultSpec {
	switch e.Kind {
	case typescript.ModelString, typescript.ModelTemplate:
		if v, ok := literalString(e); ok {
			return defaultSpec{kind: defString, value: v}
		}
	case typescript.ModelNumber:
		if v, ok := jsNumber(e.Value); ok {
			return defaultSpec{kind: defNumber, value: v}
		}
		return defaultSpec{why: "the number literal " + e.Value + " is not read exactly"}
	case typescript.ModelBoolean:
		return defaultSpec{kind: defBoolean, value: e.Value}
	case typescript.ModelNull:
		return defaultSpec{kind: defNull}
	case typescript.ModelObject, typescript.ModelArray:
		if v, ok := jsonStringify(e); ok {
			return defaultSpec{kind: defJSON, value: v}
		}
		return defaultSpec{why: "the object or array default is not a JSON literal"}
	case typescript.ModelArrow, typescript.ModelFunction:
		return defaultSpec{kind: defClient, value: "function"}
	case typescript.ModelIdentifier:
		if isUndefined(e) {
			return defaultSpec{kind: defAbsent}
		}
	case typescript.ModelMember:
		if s.isTypes(e.Object) && clientDefaults[e.Name] {
			return defaultSpec{kind: defClient, value: e.Name}
		}
	case typescript.ModelCall:
		c := e.Callee
		if c.Kind == typescript.ModelMember && s.isTypes(c.Object) && clientDefaults[c.Name] && len(e.Args) == 0 {
			return defaultSpec{kind: defClient, value: c.Name}
		}
		if c.Kind == typescript.ModelMember && (c.Name == "literal" || c.Name == "fn") && s.isSequelize(c.Object) {
			return sqlDefault(c.Name, e.Args)
		}
	}
	return defaultSpec{why: "the default is not a literal (a constant, an enum member or an expression: open point O5)"}
}

// clientDefaults are the DataTypes defaults Sequelize fills in the client
// (lib/utils.js:234-245: no DEFAULT in the SQL).
var clientDefaults = map[string]bool{"NOW": true, "UUIDV4": true, "UUIDV1": true}

// sqlDefault reads `literal("text")` and `fn("name", literal args...)`.
func sqlDefault(kind string, args []typescript.ModelExpr) defaultSpec {
	if len(args) == 0 {
		return defaultSpec{why: kind + "() without arguments is not read"}
	}
	text, ok := literalString(&args[0])
	if !ok {
		return defaultSpec{why: kind + "(...) with a non-literal first argument is not read"}
	}
	if kind == defLiteral {
		if len(args) != 1 {
			return defaultSpec{why: "literal(...) with several arguments is not read"}
		}
		return defaultSpec{kind: defLiteral, value: text}
	}
	all := text + "("
	for i := 1; i < len(args); i++ {
		switch args[i].Kind {
		case typescript.ModelString, typescript.ModelNumber, typescript.ModelBoolean:
			all += " " + args[i].Value
		default:
			return defaultSpec{why: "fn(...) with an argument that is not a literal is not read"}
		}
	}
	return defaultSpec{kind: defFn, value: all + ")"}
}

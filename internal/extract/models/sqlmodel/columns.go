package sqlmodel

import (
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/lang/facts"
)

// column is one model column: a member of the table class's chain that maps
// to a database column. Unknown facts are nil in attrs.
type column struct {
	name, attribute string
	file            string // the member's file
	line            int
	unknown         bool
	reasons         []reason
	attrs           map[string]any
	fks             []*foreignKey
	fkUnknown       bool
	identity        string
	// nullableFromPK marks K2's nullability derived from primary_key (no
	// nullable= keyword): a PrimaryKeyConstraint would change it.
	nullableFromPK bool
	// refused is true when an X1 attribute assignment may rename the
	// column (Rev 3.2): it gets no node.
	refused bool
	// claims holds, per claim fact, the contract row whose branch last set
	// it (T018g0); the core emits a claim only for a fact still known.
	claims map[string]string
	// f5Source is the source type F5 mapped the type from (a canonical
	// name in f5Types, or the enum base of an enum class), empty when F5
	// did not set the type. TestModelClaims keys F5's oracle forms by it
	// (T018g0 Revision 2): two source types that map to one class are two
	// forms.
	f5Source string
}

// set writes one column fact and records the row whose branch set it; a
// nil value is unknown and drops the fact's claim.
func (c *column) set(row, fact string, v any) {
	c.attrs[fact] = v
	if fact == "type" && (row != "F5" || v == nil) {
		c.f5Source = ""
	}
	if v == nil {
		delete(c.claims, fact)
		return
	}
	c.claims[fact] = row
}

type foreignKey struct {
	schema, table, column string
	hasSchema             bool
	onDelete              any
	status                string
	target                string
	file                  string
	line                  int
}

// annInfo is the reading of an annotation by rows A1, A2, A3, A5 and A1m.
type annInfo struct {
	optional *bool
	inner    facts.PythonModelExpr
	file     string
	scope    int
	dColumn  bool // A1m: a non-plain metadata element
	classVar bool
	mapped   bool
	multi    bool // a union with more than one non-None member
	unknown  bool // A3
}

func boolp(b bool) *bool { return &b }

// nullabilityRow is the claim row of a nullability the annotation states
// (T018g0 Revision 2): A1 (Optional or a union with None: nullable) or A2
// (not nullable). F2 decides that the annotation applies; the claim names
// the row that states the value.
func nullabilityRow(optional bool) string {
	if optional {
		return "A1"
	}
	return "A2"
}

func (l *linker) annotation(file string, scope, line int, e facts.PythonModelExpr, depth int, outer bool) annInfo {
	if depth > 5 {
		return annInfo{unknown: true}
	}
	switch e.Kind {
	case "name":
		r := l.resolve(file, scope, e.Name)
		if r.canonical != "" || r.class != nil {
			tag("A2")
			return annInfo{optional: boolp(false), inner: e, file: file, scope: scope}
		}
		if alias, aliasFile, ok := l.aliasExpr(r, line, file); ok {
			tag("A5")
			return l.annotation(aliasFile, 0, 1<<30, alias, depth+1, outer)
		}
		tag("A3")
		return annInfo{unknown: true}
	case "subscript":
		base := l.resolve(file, scope, e.Name).canonical
		switch base {
		case "typing.Annotated":
			tag("A1m")
			if len(e.Args) == 0 {
				return annInfo{unknown: true}
			}
			info := l.annotation(file, scope, line, e.Args[0], depth+1, false)
			for _, m := range e.Args[1:] {
				if !l.plainMetadata(file, scope, m) {
					info.dColumn = true
				}
			}
			return info
		case "typing.ClassVar":
			return annInfo{classVar: true}
		case "sqlalchemy.orm.Mapped":
			return annInfo{mapped: true}
		case "typing.Optional":
			if len(e.Args) != 1 {
				return annInfo{unknown: true}
			}
			info := l.annotation(file, scope, line, e.Args[0], depth+1, false)
			tag("A1")
			info.optional = boolp(true)
			return info
		case "typing.Union":
			return l.union(file, scope, line, e.Args, depth)
		}
		tag("A2")
		return annInfo{optional: boolp(false), inner: e, file: file, scope: scope}
	case "union":
		return l.union(file, scope, line, e.Args, depth)
	}
	tag("A3")
	return annInfo{unknown: true}
}

func isNone(e facts.PythonModelExpr) bool {
	return e.Kind == "literal" && e.LitKind == "none" || e.Kind == "name" && e.Name == "None"
}

func (l *linker) union(file string, scope, line int, args []facts.PythonModelExpr, depth int) annInfo {
	var rest []facts.PythonModelExpr
	none := false
	for _, a := range args {
		if isNone(a) {
			none = true
		} else {
			rest = append(rest, a)
		}
	}
	var info annInfo
	switch len(rest) {
	case 0:
		return annInfo{unknown: true}
	case 1:
		info = l.annotation(file, scope, line, rest[0], depth+1, false)
	default:
		info = annInfo{multi: true}
		for _, a := range rest {
			sub := l.annotation(file, scope, line, a, depth+1, false)
			if sub.dColumn {
				info.dColumn = true
			}
			if sub.unknown {
				info.unknown = true
			}
		}
	}
	if none {
		tag("A1")
		info.optional = boolp(true)
	} else if len(rest) > 1 {
		info.optional = boolp(false)
	}
	return info
}

var plainValidators = map[string]bool{"pydantic.AfterValidator": true, "pydantic.BeforeValidator": true, "pydantic.PlainValidator": true, "pydantic.WrapValidator": true}

// plainMetadata decides A1m's "plain" element: a constant or a pydantic
// validator call, directly or through a name bound once to such a call.
func (l *linker) plainMetadata(file string, scope int, e facts.PythonModelExpr) bool {
	switch e.Kind {
	case "literal":
		return true
	case "call":
		return plainValidators[l.canon(file, scope, &e)]
	case "name":
		r := l.resolve(file, scope, e.Name)
		if callee, f, ok := l.callBinding(r); ok {
			return plainValidators[l.resolve(f, 0, callee).canonical]
		}
	}
	return false
}

// f5Types is SQLModel's get_sqlalchemy_type mapping for resolved names
// (sqlmodel/main.py 686-737); enum classes are checked first.
var f5Types = map[string]string{
	"builtins.str": "AutoString", "pydantic.EmailStr": "AutoString", "pathlib.Path": "AutoString",
	"ipaddress.IPv4Address": "AutoString", "ipaddress.IPv4Network": "AutoString", "ipaddress.IPv6Address": "AutoString", "ipaddress.IPv6Network": "AutoString",
	"builtins.float": "Float", "builtins.bool": "Boolean", "builtins.int": "Integer",
	"datetime.datetime": "DateTime", "datetime.date": "Date", "datetime.timedelta": "Interval", "datetime.time": "Time",
	"builtins.bytes": "LargeBinary", "decimal.Decimal": "Numeric", "uuid.UUID": "Uuid",
}

var failingBuiltins = map[string]bool{"builtins.dict": true, "builtins.list": true, "builtins.set": true, "builtins.tuple": true, "builtins.object": true, "builtins.type": true, "builtins.frozenset": true}

var genericBases = map[string]bool{"builtins.dict": true, "builtins.list": true, "builtins.set": true, "builtins.tuple": true, "builtins.frozenset": true, "typing.List": true, "typing.Dict": true, "typing.Set": true, "typing.Tuple": true, "typing.Literal": true, "typing.Sequence": true}

// mapType applies F5 to the inner type: the SQLAlchemy type class and the
// source type it maps (the canonical name, or the enum base of an enum
// class), or "" when unproven; failure is true when the mapping proven
// fails (rule P1).
func (l *linker) mapType(info annInfo) (t, source string, fails bool) {
	if info.multi {
		return "", "", !info.unknown
	}
	e := info.inner
	switch e.Kind {
	case "name":
		r := l.resolve(info.file, info.scope, e.Name)
		if r.class != nil {
			if base := l.enumBase(*r.class, 0); base != "" {
				return "Enum", base, false
			}
			return "", "", false
		}
		if t, ok := f5Types[r.canonical]; ok {
			return t, r.canonical, false
		}
		return "", "", failingBuiltins[r.canonical]
	case "subscript":
		return "", "", genericBases[l.resolve(info.file, info.scope, e.Name).canonical]
	}
	return "", "", false
}

var enumBases = map[string]bool{"enum.Enum": true, "enum.StrEnum": true, "enum.IntEnum": true, "enum.Flag": true, "enum.IntFlag": true}

// enumBase returns the enum base (a key of enumBases) a repository class
// derives from, or "" when none is proven.
func (l *linker) enumBase(k classKey, depth int) string {
	if depth > 20 {
		return ""
	}
	p := l.in.Python[k.file].Python
	if p == nil || k.scope >= len(p.Scopes) {
		return ""
	}
	b, ok := p.Scopes[k.scope].Bindings[k.name]
	if !ok {
		return ""
	}
	for _, base := range b.Bases {
		if base.Kind != "identifier" && base.Kind != "attribute" {
			continue
		}
		r := l.resolve(k.file, k.scope, base.Text)
		if enumBases[r.canonical] {
			return r.canonical
		}
		if r.class != nil {
			if base := l.enumBase(*r.class, depth+1); base != "" {
				return base
			}
		}
	}
	return ""
}

// fieldSignature is sqlmodel.Field's keyword set in 0.0.39 (inspect.signature).
var fieldSignature = map[string]bool{
	"alias": true, "allow_mutation": true, "const": true, "decimal_places": true, "default": true, "default_factory": true,
	"description": true, "discriminator": true, "exclude": true, "foreign_key": true, "ge": true, "gt": true, "include": true,
	"index": true, "le": true, "lt": true, "max_digits": true, "max_items": true, "max_length": true, "min_items": true,
	"min_length": true, "multiple_of": true, "nullable": true, "ondelete": true, "primary_key": true, "regex": true,
	"repr": true, "sa_column": true, "sa_column_args": true, "sa_column_kwargs": true, "sa_type": true, "schema_extra": true,
	"serialization_alias": true, "title": true, "unique": true, "unique_items": true, "validation_alias": true,
}

// kMeta is F9's metadata keywords (no column fact except F5 lengths).
var kMeta = map[string]bool{"description": true, "title": true, "exclude": true, "include": true, "repr": true, "alias": true, "schema_extra": true, "ge": true, "gt": true, "le": true, "lt": true, "min_length": true, "max_length": true, "max_digits": true, "decimal_places": true}

// kLen is K-len: the keywords that keep max_length readable for AutoString.
var kLen = map[string]bool{"primary_key": true, "nullable": true, "index": true, "unique": true, "foreign_key": true, "ondelete": true, "default": true, "default_factory": true, "description": true, "title": true, "exclude": true, "repr": true, "alias": true, "min_length": true, "max_length": true}

var saColumnConflicts = []string{"primary_key", "nullable", "foreign_key", "ondelete", "unique", "index", "sa_type", "sa_column_args", "sa_column_kwargs"}

func litBool(e facts.PythonModelExpr) (bool, bool) {
	if e.Kind == "literal" && e.LitKind == "bool" {
		return e.Literal == "True", true
	}
	return false, false
}

func litInt(e facts.PythonModelExpr) (int, bool) {
	if e.Kind != "literal" || e.LitKind != "int" {
		return 0, false
	}
	n, err := strconv.Atoi(e.Literal)
	return n, err == nil
}

func kw(e *facts.PythonModelExpr, name string) (facts.PythonModelExpr, bool) {
	for _, k := range e.Keywords {
		if k.Name == name {
			return k.Value, true
		}
	}
	return facts.PythonModelExpr{}, false
}

// newColumn starts a column with every fact unknown.
func newColumn(name, file string, line int) *column {
	return &column{name: name, attribute: name, file: file, line: line, claims: map[string]string{}, attrs: map[string]any{
		"type": nil, "length": nil, "timezone": nil, "precision": nil, "scale": nil,
		"nullable": nil, "primary_key": nil, "index": nil, "unique": nil,
		"default": nil, "server_default": nil,
	}}
}

// openForeignKeys makes the column's foreign-key list unknown: a mutation
// site may add a foreign key to it (X1, Rev 3.1).
func (c *column) openForeignKeys(why string) {
	if c.unknown || c.fkUnknown {
		return
	}
	tag("X1")
	c.fkUnknown = true
	c.reasons = append(c.reasons, reason{Row: "X1", Fact: fact("foreign_keys"), Line: c.line, File: c.file, Reason: why})
}

func (c *column) makeUnknown(row string, why string) {
	tag(row)
	c.unknown = true
	c.fkUnknown = true
	c.fks = nil
	for k := range c.attrs {
		if k != "annotation" {
			c.attrs[k] = nil
		}
	}
	c.claims = map[string]string{}
	c.reasons = append(c.reasons, reason{Row: row, Line: c.line, File: c.file, Reason: why})
}

// fieldColumn claims a column from an annotated member (rows M2-M4, A*, F*,
// K*, T1). failure names a P1 proven failure.
func (l *linker) fieldColumn(file string, scope int, m facts.PythonModelMember) (col *column, skip bool, failure string) {
	col = newColumn(m.Name, file, m.Line)
	if m.Annotation != nil && m.Annotation.Text != "" {
		col.attrs["annotation"] = m.Annotation.Text
	}
	if m.Annotation == nil {
		// The capture keeps no annotation for a Relationship member (M5).
		if m.Value != nil && m.Value.Kind == "call" && l.canon(file, scope, m.Value) == "sqlmodel.Relationship" {
			tag("M5")
			return nil, true, ""
		}
		col.makeUnknown("D", "annotation not captured")
		return col, false, ""
	}
	info := l.annotation(file, scope, m.Line, *m.Annotation, 0, true)
	if info.classVar {
		tag("M6")
		return nil, true, ""
	}
	if info.mapped {
		col.makeUnknown("U2", "Mapped annotation")
		return col, false, ""
	}
	v := m.Value
	var call *facts.PythonModelExpr
	// memberRow is the row of the member's form (M2-M4): it claims the
	// column's name and the absences no other row states.
	memberRow := ""
	switch {
	case v == nil:
		tag("M3")
		memberRow = "M3"
		col.set("M3", "default", "absent")
	case v.Kind == "literal" && v.LitKind != "bytes" && v.LitKind != "ellipsis":
		tag("M4")
		memberRow = "M4"
		if v.LitKind == "none" {
			col.set("M4", "default", "absent")
		} else {
			col.set("M4", "default", "scalar")
			col.attrs["default_value"] = v.Literal
		}
	case v.Kind == "call":
		switch l.canon(file, scope, v) {
		case "sqlmodel.Field":
			tag("M2")
			memberRow = "M2"
			call = v
		case "sqlmodel.Relationship":
			tag("M5")
			return nil, true, ""
		case "sqlalchemy.orm.mapped_column", "sqlalchemy.orm.relationship":
			col.makeUnknown("U2", "mapped_column/relationship value")
			return col, false, ""
		default:
			col.makeUnknown("D", "value is a call the contract does not read")
			return col, false, ""
		}
	default:
		col.makeUnknown("D", "value expression the contract does not read")
		return col, false, ""
	}
	if info.dColumn {
		col.makeUnknown("A1m", "Annotated metadata that is not a constant or a pydantic validator")
		return col, false, ""
	}
	// Flags default to absent (F1) unless the Field says otherwise.
	col.claims["name"] = memberRow
	col.set("F1", "primary_key", false)
	col.set("F1", "index", false)
	col.set("F1", "unique", false)
	col.set(memberRow, "server_default", false)
	col.fks = []*foreignKey{}
	col.claims["foreign_keys"] = memberRow
	if call != nil {
		if f := l.fieldCall(file, scope, col, call, info); f != "" {
			return nil, false, f
		}
		return col, false, ""
	}
	// M3/M4 without Field: annotation decides nullability (F2, claimed by
	// the annotation row that states it), F5 the type.
	tag("F2")
	if info.optional != nil {
		col.set(nullabilityRow(*info.optional), "nullable", *info.optional)
	}
	if info.unknown {
		col.set("F2", "nullable", nil)
	}
	t, source, fails := l.mapType(info)
	if fails {
		return nil, false, "F5: the annotation's type has no SQLAlchemy mapping (get_sqlalchemy_type raises)"
	}
	l.setType(col, t, source, nil, nil)
	return col, false, ""
}

func (l *linker) setType(col *column, t, source string, call *facts.PythonModelExpr, maxLen *facts.PythonModelExpr) {
	if t == "" {
		return
	}
	tag("F5")
	col.set("F5", "type", t)
	col.f5Source = source
	switch t {
	case "AutoString":
		col.set("F5", "length", nil)
		if call == nil {
			col.set("F5", "length", "none")
			return
		}
		for _, k := range call.Keywords {
			if !kLen[k.Name] {
				return
			}
		}
		if maxLen == nil {
			col.set("F5", "length", "none")
			return
		}
		if n, ok := litInt(*maxLen); ok {
			if n > 0 {
				col.set("F5", "length", n)
			} else {
				col.set("F5", "length", "none")
			}
		}
	case "DateTime":
		col.set("F5", "timezone", false)
	case "Numeric":
		col.set("F5", "precision", "none")
		col.set("F5", "scale", "none")
		if call == nil {
			return
		}
		for key, attr := range map[string]string{"max_digits": "precision", "decimal_places": "scale"} {
			if v, ok := kw(call, key); ok {
				if n, ok := litInt(v); ok {
					col.set("F5", attr, n)
				} else {
					col.set("F5", attr, nil)
				}
			}
		}
	}
}

// fieldCall reads one sqlmodel.Field call (rows F1-F9).
func (l *linker) fieldCall(file string, scope int, col *column, call *facts.PythonModelExpr, info annInfo) string {
	has := map[string]facts.PythonModelExpr{}
	for _, k := range call.Keywords {
		if k.Name == "" {
			col.makeUnknown("D", "a ** splat in Field")
			return ""
		}
		if !fieldSignature[k.Name] {
			tag("L7")
			return "F9: keyword " + k.Name + " is outside sqlmodel.Field's signature (TypeError)"
		}
		has[k.Name] = k.Value
	}
	if _, ok := has["sa_column"]; ok {
		for _, name := range saColumnConflicts {
			if _, conflict := has[name]; conflict {
				tag("F7r")
				tag("L7")
				return "F7r: sa_column with " + name + " (SQLModel raises RuntimeError)"
			}
		}
	}
	if len(call.Args) > 1 {
		col.makeUnknown("D", "more than one positional Field argument")
		return ""
	}
	for name := range has {
		switch name {
		case "primary_key", "nullable", "index", "unique", "default", "default_factory", "foreign_key", "ondelete", "sa_type", "sa_column":
		case "sa_column_args", "sa_column_kwargs":
			col.makeUnknown("F8", name)
			return ""
		default:
			if !kMeta[name] {
				col.makeUnknown("F9", "Field keyword "+name+" the contract does not read")
				return ""
			}
			tag("F9")
		}
	}
	if sa, ok := has["sa_column"]; ok {
		tag("F7")
		if sa.Kind != "call" || l.canon(file, scope, &sa) != "sqlalchemy.Column" {
			col.makeUnknown("F7", "sa_column is not a Column(...) call")
			return ""
		}
		l.columnCall(file, scope, col, &sa)
		return ""
	}
	// F1 flags.
	for _, flag := range []string{"primary_key", "index", "unique"} {
		if v, ok := has[flag]; ok {
			tag("F1")
			if b, ok := litBool(v); ok {
				col.set("F1", flag, b)
			} else {
				col.set("F1", flag, nil)
			}
		}
	}
	// F2 nullability.
	tag("F2")
	if v, ok := has["nullable"]; ok {
		tag("F1")
		if b, ok := litBool(v); ok {
			col.set("F2", "nullable", b)
		}
	} else if pk := col.attrs["primary_key"]; pk == true {
		col.set("F2", "nullable", false)
	} else if pk == nil {
		col.set("F2", "nullable", nil)
	} else if !info.unknown && info.optional != nil {
		col.set(nullabilityRow(*info.optional), "nullable", *info.optional)
	}
	// F3, F3b defaults.
	col.set("F3", "default", "absent")
	if v, ok := has["default_factory"]; ok {
		tag("F3b")
		switch {
		case v.Kind == "literal" && v.LitKind == "none":
			col.set("F3b", "default", "absent")
		case v.Kind == "lambda":
			col.set("F3b", "default", "callable")
		case v.Kind == "name" && l.callable(file, scope, v.Name):
			col.set("F3b", "default", "callable")
		default:
			col.set("F3b", "default", nil)
		}
	} else {
		var d *facts.PythonModelExpr
		if v, ok := has["default"]; ok {
			d = &v
		} else if len(call.Args) == 1 {
			d = &call.Args[0]
		}
		if d != nil {
			tag("F3")
			switch {
			case d.Kind == "literal" && (d.LitKind == "none" || d.LitKind == "ellipsis"):
			case d.Kind == "literal" && d.LitKind != "bytes":
				col.set("F3", "default", "scalar")
				col.attrs["default_value"] = d.Literal
			default:
				col.set("F3", "default", nil)
			}
		}
	}
	// F4 foreign key.
	if v, ok := has["foreign_key"]; ok {
		tag("F4")
		var onDelete any
		if od, ok := has["ondelete"]; ok {
			if od.Kind == "literal" && od.LitKind == "str" {
				onDelete = od.Literal
				if strings.EqualFold(od.Literal, "SET NULL") && col.attrs["nullable"] == false {
					tag("L7")
					return "F4: ondelete=\"SET NULL\" on a column that is not nullable (SQLModel raises RuntimeError)"
				}
			} else {
				col.fkUnknown = true
			}
		}
		if fk := splitTarget(v); fk != nil && !col.fkUnknown {
			fk.onDelete = onDelete
			fk.file, fk.line = col.file, col.line
			col.fks = append(col.fks, fk)
			col.claims["foreign_keys"] = "F4"
		} else {
			col.fkUnknown = true
		}
	}
	// F5/F6 type.
	if st, ok := has["sa_type"]; ok {
		tag("F6")
		l.typeExpr(file, scope, col, st)
		if info.optional != nil && *info.optional && col.attrs["nullable"] == nil && !info.unknown {
			col.set(nullabilityRow(true), "nullable", true)
		}
		return ""
	}
	t, source, fails := l.mapType(info)
	if fails {
		tag("L7")
		return "F5: the annotation's type has no SQLAlchemy mapping (get_sqlalchemy_type raises)"
	}
	var maxLen *facts.PythonModelExpr
	if v, ok := has["max_length"]; ok {
		maxLen = &v
	}
	l.setType(col, t, source, call, maxLen)
	if t == "Numeric" {
		for _, k := range call.Keywords {
			if !kLen[k.Name] && k.Name != "max_digits" && k.Name != "decimal_places" {
				col.set("F5", "precision", nil)
				col.set("F5", "scale", nil)
			}
		}
	}
	return ""
}

// callable decides F3b's name case: a repository function or class, an
// imported callable or a builtin.
func (l *linker) callable(file string, scope int, name string) bool {
	r := l.resolve(file, scope, name)
	if r.canonical != "" || r.class != nil {
		return true
	}
	return r.binding.File != "" && r.binding.Identity != "" && strings.HasPrefix(r.binding.Identity, "function:")
}

// splitTarget applies F4's last-dot split (Rev 1 B4, Rev 2 re-check).
func splitTarget(v facts.PythonModelExpr) *foreignKey {
	if v.Kind != "literal" || v.LitKind != "str" || strings.ContainsAny(v.Literal, "\"'`") {
		return nil
	}
	i := strings.LastIndex(v.Literal, ".")
	if i <= 0 || i == len(v.Literal)-1 {
		return nil
	}
	fk := &foreignKey{column: v.Literal[i+1:], table: v.Literal[:i]}
	if j := strings.LastIndex(fk.table, "."); j >= 0 {
		fk.schema, fk.table, fk.hasSchema = fk.table[:j], fk.table[j+1:], true
	}
	return fk
}

// tList is T1's SQLAlchemy type classes.
var tList = map[string]string{
	"sqlalchemy.Uuid": "Uuid", "sqlalchemy.String": "String", "sqlalchemy.Text": "Text", "sqlalchemy.Unicode": "Unicode",
	"sqlalchemy.UnicodeText": "UnicodeText", "sqlalchemy.Integer": "Integer", "sqlalchemy.BigInteger": "BigInteger",
	"sqlalchemy.SmallInteger": "SmallInteger", "sqlalchemy.Boolean": "Boolean", "sqlalchemy.DateTime": "DateTime",
	"sqlalchemy.Date": "Date", "sqlalchemy.Time": "Time", "sqlalchemy.Interval": "Interval", "sqlalchemy.Float": "Float",
	"sqlalchemy.Numeric": "Numeric", "sqlalchemy.JSON": "JSON", "sqlalchemy.LargeBinary": "LargeBinary",
	"sqlalchemy.Enum": "Enum", "sqlmodel.AutoString": "AutoString",
}

// typeClass returns T1's class for a type expression, or "".
func (l *linker) typeClass(file string, scope int, e facts.PythonModelExpr) string {
	if e.Kind != "name" && e.Kind != "call" {
		return ""
	}
	return tList[l.canon(file, scope, &e)]
}

// typeExpr reads a T1 expression into col; anything else leaves the type
// unknown (K1b).
func (l *linker) typeExpr(file string, scope int, col *column, e facts.PythonModelExpr) bool {
	t := l.typeClass(file, scope, e)
	if t == "" {
		tag("K1b")
		return false
	}
	tag("T1")
	col.set("T1", "type", t)
	switch t {
	case "String", "AutoString", "Unicode":
		col.set("T1", "length", "none")
	case "DateTime":
		col.set("T1", "timezone", false)
	case "Numeric":
		col.set("T1", "precision", "none")
		col.set("T1", "scale", "none")
	}
	if e.Kind != "call" {
		return true
	}
	if t == "Enum" {
		return true
	}
	known := map[string]bool{}
	switch t {
	case "String", "AutoString", "Unicode":
		known["length"] = true
		if len(e.Args) > 0 {
			if n, ok := litInt(e.Args[0]); ok && len(e.Args) == 1 {
				col.set("T1", "length", n)
			} else {
				col.set("T1", "length", nil)
			}
		}
		if v, ok := kw(&e, "length"); ok {
			if n, ok := litInt(v); ok {
				col.set("T1", "length", n)
			} else {
				col.set("T1", "length", nil)
			}
		}
	case "DateTime":
		known["timezone"] = true
		if len(e.Args) > 0 {
			col.set("T1", "timezone", nil)
		}
		if v, ok := kw(&e, "timezone"); ok {
			if b, ok := litBool(v); ok {
				col.set("T1", "timezone", b)
			} else {
				col.set("T1", "timezone", nil)
			}
		}
	default:
		if len(e.Args) > 0 || len(e.Keywords) > 0 {
			tag("K1b")
			for _, k := range []string{"length", "timezone", "precision", "scale"} {
				col.set("T1", k, nil)
			}
		}
		return true
	}
	for _, k := range e.Keywords {
		if !known[k.Name] {
			tag("K1b")
			for _, a := range []string{"length", "timezone"} {
				col.set("T1", a, nil)
			}
		}
	}
	return true
}

// columnCall reads a sqlalchemy.Column call (rows K1-K6): the column is
// exactly what the call says (F7: Field defaults never reach it).
func (l *linker) columnCall(file string, scope int, col *column, call *facts.PythonModelExpr) {
	col.set("K2", "primary_key", false)
	col.set("K2", "index", false)
	col.set("K2", "unique", false)
	col.set("K4", "default", "absent")
	col.set("K4", "server_default", false)
	col.fks = []*foreignKey{}
	col.claims["foreign_keys"] = "K1"
	args := call.Args
	tag("K1")
	if len(args) > 0 && args[0].Kind == "literal" && args[0].LitKind == "str" {
		col.name = args[0].Literal
		col.claims["name"] = "K1"
		args = args[1:]
	}
	for i, a := range args {
		if i == 0 && l.typeClass(file, scope, a) != "" {
			l.typeExpr(file, scope, col, a)
			continue
		}
		if a.Kind == "call" && l.canon(file, scope, &a) == "sqlalchemy.ForeignKey" {
			tag("K3")
			if len(a.Args) != 1 {
				col.fkUnknown = true
				continue
			}
			fk := splitTarget(a.Args[0])
			for _, k := range a.Keywords {
				if k.Name != "ondelete" || k.Value.Kind != "literal" || k.Value.LitKind != "str" {
					fk = nil
				} else if fk != nil {
					fk.onDelete = k.Value.Literal
				}
			}
			if fk == nil {
				col.fkUnknown = true
				continue
			}
			fk.file, fk.line = col.file, col.line
			col.fks = append(col.fks, fk)
			col.claims["foreign_keys"] = "K3"
			continue
		}
		if i == 0 && l.opaqueType(file, scope, a) {
			tag("K1b")
			continue
		}
		if i == 0 && l.typeChainAlias(file, scope, a, col.line) {
			// Revision 3.3 (G-2): the column is read, its type unknown.
			tag("K1b/alias")
			col.set("K1b", "type", nil)
			col.reasons = append(col.reasons, reason{Row: "K1b", Fact: fact("type"), Line: col.line, File: col.file, Reason: "type " + a.Name + " is a module-level type chain the contract does not read"})
			continue
		}
		col.makeUnknown("K6", "Column positional argument the contract does not read")
		return
	}
	for _, k := range call.Keywords {
		switch k.Name {
		case "name":
			if k.Value.Kind == "literal" && k.Value.LitKind == "str" && len(call.Args) > 0 && call.Args[0].Kind == "literal" && call.Args[0].LitKind == "str" {
				col.makeUnknown("K6", "two column names")
				return
			}
			if k.Value.Kind == "literal" && k.Value.LitKind == "str" {
				col.name = k.Value.Literal
				col.claims["name"] = "K1"
			} else {
				col.makeUnknown("K6", "non-literal column name")
				return
			}
		case "primary_key", "nullable", "index", "unique":
			tag("K2")
			if b, ok := litBool(k.Value); ok {
				col.set("K2", k.Name, b)
			} else {
				col.set("K2", k.Name, nil)
			}
		case "default":
			tag("K4")
			switch {
			case k.Value.Kind == "literal" && k.Value.LitKind == "none":
			case k.Value.Kind == "literal" && k.Value.LitKind != "bytes" && k.Value.LitKind != "ellipsis":
				col.set("K4", "default", "scalar")
				col.attrs["default_value"] = k.Value.Literal
			case k.Value.Kind == "lambda":
				col.set("K4", "default", "callable")
			default:
				col.set("K4", "default", nil)
			}
		case "server_default":
			tag("K4")
			col.set("K4", "server_default", true)
			if k.Value.Kind == "literal" && k.Value.LitKind == "str" {
				col.attrs["server_default_text"] = k.Value.Literal
			} else if k.Value.Kind == "call" && l.canon(file, scope, &k.Value) == "sqlalchemy.text" && len(k.Value.Args) == 1 && k.Value.Args[0].Kind == "literal" && k.Value.Args[0].LitKind == "str" {
				col.attrs["server_default_text"] = k.Value.Args[0].Literal
			}
		case "onupdate", "server_onupdate":
			tag("K4")
		case "comment", "doc", "info", "autoincrement":
			tag("K5")
		default:
			col.makeUnknown("K6", "Column keyword "+k.Name)
			return
		}
	}
	tag("K2")
	if _, ok := kw(call, "nullable"); !ok {
		col.nullableFromPK = true
		switch col.attrs["primary_key"] {
		case true:
			col.set("K2", "nullable", false)
		case false:
			col.set("K2", "nullable", true)
		default:
			col.set("K2", "nullable", nil)
		}
	}
}

// typeChainAlias decides K1b's module-level type alias (Revision 3.3, G-2):
// e names a module-level binding, assigned once and before the column's
// line when in the same file, whose value is a T1 class call followed only
// by `.with_variant(...)` calls (`JsonVariant = JSON().with_variant(JSONB(),
// "postgresql")`). Such a value is a type, so the positional argument is
// the column type and no schema item (ForeignKey, Sequence, Identity,
// Computed) the column's other facts would miss. Any other bound value
// stays K6.
func (l *linker) typeChainAlias(file string, scope int, e facts.PythonModelExpr, line int) bool {
	if e.Kind != "name" {
		return false
	}
	r := l.resolve(file, scope, e.Name)
	b := r.binding
	if r.class != nil || r.canonical != "" || b.File == "" || b.External || b.Syntax.Import != nil || b.Syntax.Count != 1 || !b.Syntax.Stable || b.Scope != 0 || b.Syntax.Value.Kind != "call" {
		return false
	}
	src := l.in.Sources[b.File]
	off := b.Syntax.Value.Offset
	if off <= 0 || off >= len(src) {
		return false
	}
	if b.File == file && lineOf(src, off) >= line {
		return false
	}
	p := &typeReader{s: string(src[off:])}
	head, ok := p.primary()
	if !ok || head.Kind != "call" || tList[l.canon(b.File, 0, &head)] == "" {
		return false
	}
	for p.i < len(p.s) && p.s[p.i] == '.' {
		p.i++
		m, ok := p.primary()
		if !ok || m.Kind != "call" || m.Name != "with_variant" {
			return false
		}
	}
	p.space(false)
	return p.i >= len(p.s) || p.s[p.i] == '\n' || p.s[p.i] == '\r' || p.s[p.i] == '#' || p.s[p.i] == ';'
}

// opaqueType decides K1b's provable-but-unread type: a repository class
// whose base resolves to TypeDecorator/UserDefinedType or a T1 class, or a
// T1 class called with arguments the pass does not read.
func (l *linker) opaqueType(file string, scope int, e facts.PythonModelExpr) bool {
	if e.Kind != "name" && e.Kind != "call" {
		return false
	}
	r := l.resolve(file, scope, e.Name)
	if r.class == nil {
		return false
	}
	p := l.in.Python[r.class.file].Python
	if p == nil || r.class.scope >= len(p.Scopes) {
		return false
	}
	b := p.Scopes[r.class.scope].Bindings[r.class.name]
	for _, base := range b.Bases {
		if base.Kind != "identifier" && base.Kind != "attribute" {
			continue
		}
		c := l.resolve(r.class.file, r.class.scope, base.Text).canonical
		if c == "sqlalchemy.TypeDecorator" || c == "sqlalchemy.types.UserDefinedType" || c == "sqlalchemy.UserDefinedType" || tList[c] != "" {
			return true
		}
	}
	return false
}

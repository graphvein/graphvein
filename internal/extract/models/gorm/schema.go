package gorm

import (
	"reflect"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/graphvein/graphvein/internal/lang/facts"
)

// This file ports GORM's schema parse (gorm.io/gorm v1.31.2
// schema/schema.go:139-349 and schema/field.go:105-445) over the Go model
// payload: the table name (row GO-K4, with TableName methods and Go
// constant folding, O7), field selection and embedding (GO-F1), column
// names (GO-F2), the GORM data type with size, precision and scale
// (GO-T1), and the primary key, autoincrement, not-null, unique and
// default settings the migrator renders (GO-C1-C4). Relations are parsed
// by relations.go (rows GO-R1-R3).

// typeInfo classifies a field's Go type as GORM's ParseField sees it.
type typeInfo struct {
	// Kind is bool, int, uint, float, string, time, bytes, struct, slice,
	// map or other.
	Kind string
	// Size is the bit size of an int, uint or float kind.
	Size int
	// Basic marks a predeclared type (byte, uint8, ...), for []byte.
	Basic bool
	// Ptr marks a pointer; Null a database/sql Null type or gorm.DeletedAt
	// (a driver.Valuer whose first field gives the data type).
	Ptr, Null bool
	// Struct is the struct of a struct kind.
	Struct *structRef
	// Elem is the element of a slice kind.
	Elem *typeInfo
	// Refuse says why the type is outside GO-T1's closed list (the column
	// is then unknown and the column set not complete); Interface marks an
	// interface type, which a serializer stores as a string.
	Refuse    string
	Interface bool
}

// structRef is a module struct, or gorm.Model.
type structRef struct {
	File, Dir string
	Struct    *facts.GoStruct
	// Model marks gorm.Model (gorm.io/gorm model.go).
	Model bool
}

func (s *structRef) key() string {
	if s.Model {
		return "gorm.io/gorm#Model"
	}
	return s.File + "#" + s.Struct.Name
}

func (s *structRef) name() string {
	if s.Model {
		return "Model"
	}
	return s.Struct.Name
}

// basicTypes are the predeclared types GO-T1 reads.
var basicTypes = map[string]typeInfo{
	"bool": {Kind: "bool"}, "string": {Kind: "string"},
	"int": {Kind: "int", Size: 64}, "int8": {Kind: "int", Size: 8}, "int16": {Kind: "int", Size: 16}, "int32": {Kind: "int", Size: 32}, "int64": {Kind: "int", Size: 64}, "rune": {Kind: "int", Size: 32},
	"uint": {Kind: "uint", Size: 64}, "uint8": {Kind: "uint", Size: 8}, "byte": {Kind: "uint", Size: 8}, "uint16": {Kind: "uint", Size: 16}, "uint32": {Kind: "uint", Size: 32}, "uint64": {Kind: "uint", Size: 64},
	"float32": {Kind: "float", Size: 32}, "float64": {Kind: "float", Size: 64},
}

// sqlNullTypes maps database/sql's Null types to the type of their first
// field, which GORM uses as the data type (field.go:139-185).
var sqlNullTypes = map[string]string{"NullString": "string", "NullInt64": "int64", "NullInt32": "int32", "NullInt16": "int16", "NullByte": "byte", "NullFloat64": "float64", "NullBool": "bool", "NullTime": "time"}

// customMethods are the methods that make a type a GORM custom type: its
// data type is then not GO-T1's (a Valuer, Scanner, Serializer or
// GormDataTypeInterface).
var customMethods = map[string]bool{"Value": true, "Scan": true, "GormDataType": true, "GormDBDataType": true}

// gfield is one parsed field (schema.Field).
type gfield struct {
	Name, DBName string
	// ColumnTag marks a DBName from the column tag (GO-F2); Prefixed one
	// with an embeddedPrefix.
	ColumnTag, Prefixed bool
	// File and Line locate the field declaration.
	File string
	Line int
	Tags map[string]string
	// Raw is the field's gorm tag.
	Raw                    string
	Type                   typeInfo
	DataType, GORMDataType string
	Size, Precision, Scale int
	PK, AI, NotNull, Uniq  bool
	HasDefault             bool
	// Default is the trimmed default text; Parsed says GORM set a
	// default value interface (a literal bound at migration).
	Default              string
	Parsed               bool
	C, U, R              bool
	IgnoreMig            bool
	BindNames            []string
	Embedded             *gschema
	EmbeddedTagged       bool
	Anonymous, Serialize bool
	// RawWhy is set for a raw type tag that is not one type name with
	// optional (n[,m]) or [] (type, nullability, default and unique
	// unknown, the constraint set partial); SerialRaw for a raw serial
	// type (nullability, default and autoincrement unknown). Revision 2,
	// A1.
	RawWhy, SerialRaw string
	// Unknown makes the column unknown (UnknownRow says by which row);
	// NameUnknown means even its name is not known (no record).
	Unknown, UnknownRow string
	NameUnknown         string
	// Schema is the top-level schema the field belongs to (embedded fields
	// included), as GORM's field.Schema.
	Schema *gschema
	// copies are the relations that copied a referenced field's type onto
	// this foreign-key field (relationship.go:592-604, row GO-R1), with the
	// state before the first one.
	copies []typeCopy
	// joinSrc is the key a join table field is built from (row GO-R2).
	joinSrc *gfield
}

// gschema is one parsed struct (schema.Schema).
type gschema struct {
	Ref  *structRef
	Name string
	// Table is the table name, or "" with TableWhy when it is not known.
	Table, TableWhy string
	// TableFromMethod marks a name from a TableName method; TableLine its
	// line.
	TableFromMethod bool
	TableFile       string
	TableLine       int
	Fields          []*gfield
	DBNames         []string
	ByDB, ByName    map[string]*gfield
	Primary         []*gfield
	Prioritized     *gfield
	Relations       []*gfield
	// Dup lists the column names more than one field writes (GO-F1).
	Dup map[string]bool
	// Failure is a provable migrate failure (GO-P1) and FailureLine its
	// line.
	Failure     string
	FailureLine int
	// Partial lists why the column set is not complete (row, line, why).
	Partial []partialReason
	// Rels are the schema's own relations in field order; Registered the
	// has-one and has-many relations of other schemas that register on it
	// when they are parsed (relationship.go:101-106, row GO-R1).
	Rels, Registered []*grel
	// Join marks a many2many join table's schema (row GO-R2); JoinOf is the
	// relation that built it.
	Join   bool
	JoinOf *grel
	// FKWhy says why the table's foreign keys are not known (a relation
	// that is not read targets or leaves it); TypeWhy holds fields whose
	// type such a relation may change.
	FKWhy   string
	TypeWhy map[*gfield]string
}

type partialReason struct {
	Row, File string
	Line      int
	Why       string
}

func (sc *gschema) lookUp(name string) *gfield {
	if f := sc.ByDB[name]; f != nil {
		return f
	}
	return sc.ByName[name]
}

func (sc *gschema) fail(line int, why string) {
	if sc.Failure == "" {
		sc.Failure, sc.FailureLine = why, line
	}
}

func (sc *gschema) partial(row, file string, line int, why string) {
	sc.Partial = append(sc.Partial, partialReason{Row: row, File: file, Line: line, Why: why})
}

// porter parses a module's structs under one naming strategy.
type porter struct {
	r     *resolver
	namer namer
	// limitTable is the longest table or column name a dialect of the
	// group keeps (63 for postgres or a runtime group with postgres;
	// GO-K4), 0 for no limit.
	limitName int
	// inflection is "" when plural-derived names are claimable, else why
	// not (an inflection.Add* call in the module).
	inflection string
	cache      map[string]*gschema
}

// classify classifies a type expression written in file.
func (p *porter) classify(file string, e facts.GoExpr, depth int) typeInfo {
	if depth > 16 {
		return typeInfo{Kind: "other", Refuse: "the type is nested too deeply"}
	}
	switch e.Kind {
	case facts.GoPointer:
		if e.Object == nil {
			break
		}
		t := p.classify(file, *e.Object, depth+1)
		t.Ptr = true
		return t
	case facts.GoIdentifier:
		if b, ok := basicTypes[e.Name]; ok {
			b.Basic = true
			return b
		}
		if e.Name == "any" || e.Name == "error" {
			return typeInfo{Kind: "other", Interface: true, Refuse: "an interface type without a serializer"}
		}
		return p.named(file, e, depth)
	case facts.GoSelector:
		path := p.r.importOf(file, e.Object)
		switch {
		case path == "time" && e.Name == "Time":
			return typeInfo{Kind: "time"}
		case path == "database/sql" && sqlNullTypes[e.Name] != "":
			first := sqlNullTypes[e.Name]
			t := typeInfo{Kind: "time"}
			if first != "time" {
				t = basicTypes[first]
				t.Basic = true
			}
			t.Null = true
			return t
		case path == pkgGorm && e.Name == "DeletedAt":
			return typeInfo{Kind: "time", Null: true}
		case path == pkgGorm && e.Name == "Model":
			return typeInfo{Kind: "struct", Struct: &structRef{Model: true}}
		}
		if _, _, _, external, ok := p.r.scope(file, e); !ok {
			if external == "" {
				external = path
			}
			return typeInfo{Kind: "other", Refuse: "the type " + external + "." + e.Name + " is outside the module (not one GO-T1 lists)"}
		}
		return p.named(file, e, depth)
	case facts.GoSlice, facts.GoArray:
		if e.Object == nil {
			break
		}
		el := p.classify(file, *e.Object, depth+1)
		if el.Kind == "uint" && el.Size == 8 && el.Basic && !el.Ptr {
			return typeInfo{Kind: "bytes"}
		}
		return typeInfo{Kind: "slice", Elem: &el}
	case facts.GoMap:
		return typeInfo{Kind: "map"}
	case facts.GoInterface:
		return typeInfo{Kind: "other", Interface: true, Refuse: "an interface type without a serializer"}
	}
	return typeInfo{Kind: "other", Refuse: "a " + e.Kind + " type expression is not one GO-T1 lists"}
}

// decl is a module type declaration.
type decl struct {
	file, dir, pkg string
	st             *facts.GoStruct
	nt             *facts.GoNamedType
}

// lookupType finds the module type a name (T or pkg.T) written in file
// names.
func (p *porter) lookupType(file string, e facts.GoExpr) (decl, bool) {
	dir, pkg, name, _, ok := p.r.scope(file, e)
	if !ok {
		return decl{}, false
	}
	for _, f := range p.r.packageFiles(dir, pkg) {
		g := p.r.d.in.Files[f].Go
		if g == nil {
			continue
		}
		for i := range g.Structs {
			if g.Structs[i].Name == name {
				return decl{file: f, dir: dir, pkg: pkg, st: &g.Structs[i]}, true
			}
		}
		for i := range g.Types {
			if g.Types[i].Name == name {
				return decl{file: f, dir: dir, pkg: pkg, nt: &g.Types[i]}, true
			}
		}
	}
	return decl{}, false
}

// methods lists the payload methods of type name in a package.
func (p *porter) methods(dir, pkg, name string) []facts.GoMethod {
	var out []facts.GoMethod
	for _, f := range p.r.packageFiles(dir, pkg) {
		g := p.r.d.in.Files[f].Go
		if g == nil {
			continue
		}
		for _, m := range g.Methods {
			if m.Receiver == name {
				out = append(out, m)
			}
		}
	}
	return out
}

// custom names the GORM custom-type method a type has, or "".
func (p *porter) custom(d decl, name string) string {
	for _, m := range p.methods(d.dir, d.pkg, name) {
		if customMethods[m.Name] {
			return m.Name
		}
	}
	return ""
}

// named classifies a module named type.
func (p *porter) named(file string, e facts.GoExpr, depth int) typeInfo {
	d, ok := p.lookupType(file, e)
	if !ok {
		return typeInfo{Kind: "other", Refuse: "the type " + e.Name + " is not declared in the module's read files"}
	}
	name := e.Name
	if m := p.custom(d, name); m != "" {
		return typeInfo{Kind: "other", Refuse: "the module type " + name + " has a " + m + " method (a GORM custom type)"}
	}
	if d.st != nil {
		if len(d.st.TypeParams) > 0 {
			return typeInfo{Kind: "other", Refuse: "the struct " + name + " is generic"}
		}
		ref := &structRef{File: d.file, Dir: d.dir, Struct: d.st}
		if why := p.promotes(ref, depth); why != "" {
			return typeInfo{Kind: "other", Refuse: "the struct " + name + " " + why}
		}
		return typeInfo{Kind: "struct", Struct: ref}
	}
	nt := d.nt
	if nt.Alias || len(nt.TypeParams) > 0 {
		return typeInfo{Kind: "other", Refuse: "the type " + name + " is an alias or generic"}
	}
	t := p.classify(d.file, nt.Type, depth+1)
	t.Basic, t.Null = false, false
	switch t.Kind {
	case "bool", "int", "uint", "float", "string", "time", "bytes":
		if t.Ptr || t.Refuse != "" {
			return typeInfo{Kind: "other", Refuse: "the type " + name + " is not one GO-T1 lists"}
		}
		return t
	}
	return typeInfo{Kind: "other", Refuse: "the type " + name + " has an underlying type GO-T1 does not list"}
}

// promotes says why a struct's embedded fields may give it methods GORM
// reads (a promoted Value, Scan or GormDataType makes the struct a custom
// type): an embedded type outside the module or one with such a method.
func (p *porter) promotes(ref *structRef, depth int) string {
	if depth > 16 {
		return "embeds too deeply"
	}
	for _, f := range ref.Struct.Fields {
		if !f.Embedded {
			continue
		}
		t := f.Type
		if t.Kind == facts.GoSelector && p.r.importOf(ref.File, t.Object) == pkgGorm && t.Name == "Model" {
			continue
		}
		d, ok := p.lookupType(ref.File, t)
		if !ok {
			return "embeds a type outside the module, whose methods are not read"
		}
		if m := p.custom(d, t.Name); m != "" {
			return "embeds " + t.Name + ", which has a " + m + " method"
		}
		if d.st != nil {
			if why := p.promotes(&structRef{File: d.file, Dir: d.dir, Struct: d.st}, depth+1); why != "" {
				return why
			}
		}
	}
	return ""
}

// gormModelFields are gorm.Model's fields (gorm.io/gorm model.go).
var gormModelFields = []struct{ name, typ, tag string }{
	{"ID", "uint", `gorm:"primarykey"`},
	{"CreatedAt", "time", ""},
	{"UpdatedAt", "time", ""},
	{"DeletedAt", "deleted", `gorm:"index"`},
}

// rawField is one struct field to parse.
type rawField struct {
	name string
	file string
	line int
	typ  typeInfo
	tag  string
	anon bool
}

// fieldsOf lists a struct's exported fields with their classified types.
func (p *porter) fieldsOf(ref *structRef, line int) []rawField {
	var out []rawField
	if ref.Model {
		for _, f := range gormModelFields {
			t := typeInfo{Kind: "time"}
			switch f.typ {
			case "uint":
				t = typeInfo{Kind: "uint", Size: 64, Basic: true}
			case "deleted":
				t.Null = true
			}
			out = append(out, rawField{name: f.name, file: "", line: line, typ: t, tag: f.tag})
		}
		return out
	}
	for _, f := range ref.Struct.Fields {
		names := f.Names
		texpr := f.Type
		if f.Embedded {
			base := f.Type
			if base.Kind == facts.GoPointer && base.Object != nil {
				base = *base.Object
			}
			switch base.Kind {
			case facts.GoIdentifier, facts.GoSelector:
				names = []string{base.Name}
			default:
				names = []string{""}
			}
			if f.Pointer {
				texpr = facts.GoExpr{Kind: facts.GoPointer, Object: &f.Type}
			}
		}
		for _, n := range names {
			if !exported(n) {
				continue
			}
			out = append(out, rawField{name: n, file: ref.File, line: f.Line, typ: p.classify(ref.File, texpr, 0), tag: f.Tag, anon: f.Embedded})
		}
	}
	return out
}

func exported(name string) bool {
	r, _ := utf8.DecodeRuneInString(name)
	return name != "" && unicode.IsUpper(r)
}

// parse parses a struct; embeddedTable is the parent's table for an
// embedded struct (its fields join the parent, schema.go:220-228).
func (p *porter) parse(ref *structRef, embedded bool, parentTable string, line, depth int) *gschema {
	if !embedded {
		if sc := p.cache[ref.key()]; sc != nil {
			return sc
		}
	}
	sc := &gschema{Ref: ref, Name: ref.name(), ByDB: map[string]*gfield{}, ByName: map[string]*gfield{}, Dup: map[string]bool{}}
	if embedded {
		sc.Table = parentTable
	} else {
		p.cache[ref.key()] = sc
		p.tableName(sc)
	}
	if depth > 8 {
		sc.fail(line, "struct "+sc.Name+" embeds structs too deeply")
		return sc
	}
	p.fill(sc, p.fieldsOf(ref, line), embedded, depth)
	if !embedded {
		p.relations(sc)
	}
	return sc
}

// fill parses a struct's raw fields into sc: the field list, the column
// names, the primary key and its prioritized field, the relation fields
// and the autoincrement default (schema.go:220-349).
func (p *porter) fill(sc *gschema, raws []rawField, embedded bool, depth int) {
	for _, rf := range raws {
		f := p.parseField(sc, rf, depth)
		if f.Embedded != nil {
			sc.Fields = append(sc.Fields, f.Embedded.Fields...)
		} else {
			sc.Fields = append(sc.Fields, f)
		}
	}
	counts := map[string]int{}
	for _, f := range sc.Fields {
		// A field whose type is not read may be a column (a custom type)
		// or a relation: it is written as an unknown column under its
		// would-be name, and the column set is not complete.
		if f.DBName == "" && (f.DataType != "" || f.Unknown != "" && (f.C || f.U || f.R) && !f.IgnoreMig) {
			f.DBName = p.namer.ColumnName(f.Name)
		}
		if f.DBName != "" {
			counts[f.DBName]++
			v, ok := sc.ByDB[f.DBName]
			if !ok || (f.C || f.U || f.R) && len(f.BindNames) < len(v.BindNames) {
				if !ok {
					sc.DBNames = append(sc.DBNames, f.DBName)
				}
				sc.ByDB[f.DBName] = f
				sc.ByName[f.Name] = f
				if v != nil && v.PK {
					for i, x := range sc.Primary {
						if x.DBName == v.DBName {
							sc.Primary = append(sc.Primary[:i], sc.Primary[i+1:]...)
							break
						}
					}
				}
				if f.PK {
					sc.Primary = append(sc.Primary, f)
				}
			}
		}
		if of, ok := sc.ByName[f.Name]; !ok || of.Tags["-"] == "-" {
			sc.ByName[f.Name] = f
		}
	}
	for n, c := range counts {
		if c > 1 {
			sc.Dup[n] = true
		}
	}
	pf := sc.lookUp("id")
	if pf == nil {
		pf = sc.lookUp("ID")
	}
	if pf != nil {
		if pf.PK {
			sc.Prioritized = pf
		} else if len(sc.Primary) == 0 {
			pf.PK = true
			sc.Prioritized = pf
			sc.Primary = append(sc.Primary, pf)
		}
	}
	if sc.Prioritized == nil {
		if len(sc.Primary) == 1 {
			sc.Prioritized = sc.Primary[0]
		} else {
			for _, f := range sc.Primary {
				if f.AI {
					sc.Prioritized = f
					break
				}
			}
		}
	}
	if !embedded {
		for _, f := range sc.Fields {
			if f.DataType == "" && f.GORMDataType == "" && (f.C || f.U || f.R) && f.Unknown == "" {
				sc.Relations = append(sc.Relations, f)
				sc.ByName[f.Name] = f
			}
		}
	}
	if f := sc.Prioritized; f != nil && (f.GORMDataType == "int" || f.GORMDataType == "uint") {
		if _, ok := f.Tags["AUTOINCREMENT"]; !ok {
			f.HasDefault = true
			f.AI = true
		}
	}
	if !embedded {
		for _, f := range sc.Fields {
			f.Schema = sc
		}
	}
}

// parseField ports ParseField for one field.
func (p *porter) parseField(sc *gschema, rf rawField, depth int) *gfield {
	raw := reflect.StructTag(rf.tag).Get("gorm")
	tags := parseTagSetting(raw, ";")
	f := &gfield{Name: rf.name, DBName: tags["COLUMN"], ColumnTag: tags["COLUMN"] != "", File: rf.file, Line: rf.line, Tags: tags, Raw: raw, BindNames: []string{rf.name}, C: true, U: true, R: true, Anonymous: rf.anon,
		PK: checkTruth(tags["PRIMARYKEY"], tags["PRIMARY_KEY"]), AI: checkTruth(tags["AUTOINCREMENT"]), HasDefault: checkTruth(tags["AUTOINCREMENT"]),
		NotNull: checkTruth(tags["NOT NULL"], tags["NOTNULL"]), Uniq: checkTruth(tags["UNIQUE"])}
	if f.File == "" {
		f.File = sc.Ref.File
	}
	t := rf.typ
	f.Type = t
	serializer := tags["JSON"]
	if serializer == "" {
		serializer = tags["SERIALIZER"]
	}
	if serializer != "" {
		switch strings.ToLower(serializer) {
		case "json", "gob", "unixtime":
			// schema/serializer.go registers these three by default.
			f.DataType = "string"
			f.Serialize = true
		default:
			if t.Refuse == "" {
				f.Unknown, f.UnknownRow = "the serializer "+serializer+" is not one GORM registers by default", "D"
			}
		}
	}
	// A serializer stores an interface, map, slice or struct as a string;
	// a refused type (outside the module, or with GORM's custom-type
	// methods) stays unknown with or without one.
	if t.Refuse != "" && f.Unknown == "" && !(f.Serialize && t.Interface) {
		f.Unknown, f.UnknownRow = t.Refuse, "GO-T1"
	}
	if v, ok := tags["DEFAULT"]; ok {
		f.HasDefault = true
		f.Default = v
	}
	if v, ok := tags["SIZE"]; ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			n = -1
		}
		f.Size = n
	}
	if v, ok := tags["PRECISION"]; ok {
		f.Precision, _ = strconv.Atoi(v)
	}
	if v, ok := tags["SCALE"]; ok {
		f.Scale, _ = strconv.Atoi(v)
	}
	f.Default = strings.TrimSpace(f.Default)
	skip := strings.Contains(f.Default, "(") && strings.Contains(f.Default, ")") || strings.ToLower(f.Default) == "null" || f.Default == ""
	parse := f.HasDefault && !skip
	bad := func(kind string) {
		sc.fail(f.Line, "the default "+strconv.Quote(f.Default)+" of "+sc.Name+"."+f.Name+" does not parse as "+kind+" (field.go:236-268)")
	}
	switch t.Kind {
	case "bool":
		f.DataType = "bool"
		if parse {
			if _, err := strconv.ParseBool(f.Default); err != nil {
				bad("bool")
			}
			f.Parsed = true
		}
	case "int":
		f.DataType = "int"
		if parse {
			if _, err := strconv.ParseInt(f.Default, 0, 64); err != nil {
				bad("int")
			}
			f.Parsed = true
		}
	case "uint":
		f.DataType = "uint"
		if parse {
			if _, err := strconv.ParseUint(f.Default, 0, 64); err != nil {
				bad("uint")
			}
			f.Parsed = true
		}
	case "float":
		f.DataType = "float"
		if parse {
			if _, err := strconv.ParseFloat(f.Default, 64); err != nil {
				bad("float")
			}
			f.Parsed = true
		}
	case "string":
		f.DataType = "string"
		if parse {
			f.Default = strings.Trim(strings.Trim(f.Default, "'"), `"`)
			f.Parsed = true
		}
	case "time":
		f.DataType = "time"
	case "bytes":
		if f.DataType == "" {
			f.DataType = "bytes"
		}
	}
	if f.GORMDataType == "" {
		f.GORMDataType = f.DataType
	}
	if v, ok := tags["TYPE"]; ok {
		switch lv := strings.ToLower(v); lv {
		case "bool", "int", "uint", "float", "string", "time", "bytes":
			f.DataType = lv
		default:
			f.DataType = v
			// GORM writes the raw text into the column definition
			// verbatim: anything after one type name may add NOT NULL, a
			// DEFAULT, UNIQUE or other constraints (Revision 2, A1).
			switch {
			case !reRawType.MatchString(v):
				f.RawWhy = "the raw type " + strconv.Quote(v) + " is not one type name with optional (n[,m]) or []: GORM writes it verbatim, so it may set NOT NULL, a default, UNIQUE or other constraints"
			case reSerialType.MatchString(v):
				f.SerialRaw = "the raw type " + strconv.Quote(v) + " is a serial type: the database makes the column NOT NULL with a sequence default"
			}
		}
	}
	if f.Size == 0 && (t.Kind == "int" || t.Kind == "uint" || t.Kind == "float") {
		f.Size = t.Size
	}
	if v, ok := tags["-"]; ok {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "-":
			f.C, f.U, f.R = false, false, false
			f.DataType = ""
		case "all":
			f.C, f.U, f.R = false, false, false
			f.DataType = ""
			f.IgnoreMig = true
		case "migration":
			f.IgnoreMig = true
		}
	}
	if v, ok := tags["->"]; ok {
		f.C, f.U = false, false
		f.R = strings.ToLower(v) != "false"
	}
	if v, ok := tags["<-"]; ok {
		f.C, f.U = true, true
		if v != "<-" {
			f.C = strings.Contains(v, "create")
			f.U = strings.Contains(v, "update")
		}
	}
	_, embedTag := tags["EMBEDDED"]
	if embedTag || f.GORMDataType != "time" && f.GORMDataType != "bytes" && !t.Null && rf.anon && (f.C || f.U || f.R) {
		switch {
		case t.Kind == "struct":
			f.C, f.U, f.R = false, false, false
			emb := p.parse(t.Struct, true, sc.Table, f.Line, depth+1)
			if emb.Failure != "" {
				sc.fail(emb.FailureLine, emb.Failure)
			}
			sc.Partial = append(sc.Partial, emb.Partial...)
			for _, ef := range emb.Fields {
				ef.BindNames = append([]string{rf.name}, ef.BindNames...)
				if t.Struct.Model {
					ef.File, ef.Line = f.File, f.Line
				}
				if prefix, ok := tags["EMBEDDEDPREFIX"]; ok && ef.DBName != "" {
					ef.DBName = prefix + ef.DBName
					ef.Prefixed = true
				}
				if ef.PK && !checkTruth(ef.Tags["PRIMARYKEY"], ef.Tags["PRIMARY_KEY"]) {
					ef.PK = false
					if v, ok := ef.Tags["AUTOINCREMENT"]; !ok || !checkTruth(v) {
						ef.AI = false
					}
					if !ef.AI && ef.Default == "" {
						ef.HasDefault = false
					}
				}
				for k, v := range tags {
					if k != "EMBEDDED" && k != "EMBEDDEDPREFIX" {
						ef.EmbeddedTagged = true
					}
					ef.Tags[k] = v
				}
			}
			f.Embedded = emb
		case t.Refuse != "":
			// An embedded type GO-T1 does not read: its columns are not
			// known (the payload holds no fields for it, or it may be a
			// custom type, which is one column).
			f.Embedded = &gschema{}
			sc.partial("GO-F1", f.File, f.Line, "the embedded field "+rf.name+" has a type the contract does not read ("+t.Refuse+"): its columns are not known")
		case t.Kind == "slice" || t.Kind == "map" || t.Kind == "other":
			sc.fail(f.Line, "the embedded field "+rf.name+" of "+sc.Name+" is not a struct (field.go:440-442)")
		}
	}
	// A field GORM would read as a relation but whose type cannot be one:
	// a slice of a non-struct type or a map without a type or serializer
	// fails the relation parse (GO-P1).
	if f.Embedded == nil && f.DataType == "" && f.Unknown == "" && (f.C || f.U || f.R) {
		switch {
		case t.Kind == "map":
			sc.fail(f.Line, "the map field "+sc.Name+"."+f.Name+" has no type or serializer: GORM cannot parse it as a relation")
		case t.Kind == "slice" && (t.Elem == nil || t.Elem.Kind != "struct"):
			if t.Elem != nil && t.Elem.Refuse != "" {
				f.Unknown, f.UnknownRow = t.Elem.Refuse, "GO-T1"
				break
			}
			sc.fail(f.Line, "the slice field "+sc.Name+"."+f.Name+" has no type or serializer: GORM cannot parse it as a relation")
		case t.Kind != "struct" && t.Kind != "slice":
			f.Unknown, f.UnknownRow = "the field "+f.Name+" has no data type GO-T1 lists", "GO-T1"
		}
	}
	return f
}

// tableName sets a top-level schema's table name (row GO-K4,
// schema.go:181-193): a TableName() method on T or *T (or promoted from an
// embedded struct) whose body returns a Go string constant, else the
// naming strategy.
func (p *porter) tableName(sc *gschema) {
	ref := sc.Ref
	if !ref.Model {
		m, file, why := p.tableMethod(ref, 0)
		switch {
		case why != "":
			sc.TableWhy = why
			return
		case m != nil:
			sc.TableFromMethod, sc.TableFile, sc.TableLine = true, file, m.Line
			v, ok := p.constant(file, m.Return, 0)
			if m.Params != 0 || m.Results != 1 || !ok {
				sc.TableWhy = "the TableName method of " + sc.Name + " is not a single return of a string constant (GO-K4, O7)"
				return
			}
			sc.Table = v
			return
		}
	}
	name := ref.name()
	if !asciiName(name) {
		sc.TableWhy = "the struct name " + name + " is not ASCII"
		return
	}
	if !p.namer.SingularTable && p.inflection != "" {
		sc.TableWhy = "the table name is a plural and " + p.inflection
		return
	}
	sc.Table = p.namer.TableName(name)
}

// tableMethod finds the TableName method of a struct: its own (T or *T),
// else the one promoted from its embedded structs at the shallowest depth
// (ambiguous when two embedded structs at that depth have one). why is set
// when an embedded type's methods are not read.
func (p *porter) tableMethod(ref *structRef, depth int) (*facts.GoMethod, string, string) {
	if ref.Model {
		return nil, "", ""
	}
	if depth > 8 {
		return nil, "", "the struct " + ref.Struct.Name + " embeds too deeply"
	}
	dir, pkg := ref.Dir, p.r.d.in.Files[ref.File].Package
	for _, f := range p.r.packageFiles(dir, pkg) {
		g := p.r.d.in.Files[f].Go
		if g == nil {
			continue
		}
		for i := range g.Methods {
			if m := &g.Methods[i]; m.Receiver == ref.Struct.Name && m.Name == "TableName" {
				return m, f, ""
			}
		}
	}
	var found *facts.GoMethod
	var foundFile string
	n := 0
	for _, fd := range ref.Struct.Fields {
		if !fd.Embedded {
			continue
		}
		t := fd.Type
		if t.Kind == facts.GoSelector && p.r.importOf(ref.File, t.Object) == pkgGorm && t.Name == "Model" {
			continue
		}
		d, ok := p.lookupType(ref.File, t)
		if !ok {
			return nil, "", "the struct " + ref.Struct.Name + " embeds " + t.Name + ", a type whose TableName method is not read"
		}
		if d.st == nil {
			for _, m := range p.methods(d.dir, d.pkg, t.Name) {
				if m.Name == "TableName" {
					return nil, "", "the struct " + ref.Struct.Name + " embeds " + t.Name + ", whose TableName method may be promoted"
				}
			}
			continue
		}
		m, file, why := p.tableMethod(&structRef{File: d.file, Dir: d.dir, Struct: d.st}, depth+1)
		if why != "" {
			return nil, "", why
		}
		if m != nil {
			found, foundFile = m, file
			n++
		}
	}
	if n > 1 {
		return nil, "", "two embedded structs of " + ref.Struct.Name + " have a TableName method (promotion depth not ported)"
	}
	if n == 1 {
		// A promoted method from a deeper level could be shadowed or
		// ambiguous only with several; one is the method GORM calls.
		return found, foundFile, ""
	}
	return nil, "", ""
}

// constant evaluates a Go string constant expression written in file: a
// string literal, a package-level const of the module's own packages
// (captured in the payload), or a + of those (T018g5 Revision 1, O7).
func (p *porter) constant(file string, e *facts.GoExpr, depth int) (string, bool) {
	if e == nil || depth > 16 {
		return "", false
	}
	switch e.Kind {
	case facts.GoString:
		return e.Value, true
	case facts.GoBinary:
		if e.Value != "+" {
			return "", false
		}
		l, ok := p.constant(file, e.Left, depth+1)
		if !ok {
			return "", false
		}
		r, ok := p.constant(file, e.Right, depth+1)
		return l + r, ok
	case facts.GoIdentifier, facts.GoSelector:
		dir, pkg, name, _, ok := p.r.scope(file, *e)
		if !ok {
			return "", false
		}
		for _, f := range p.r.packageFiles(dir, pkg) {
			g := p.r.d.in.Files[f].Go
			if g == nil {
				continue
			}
			for i := range g.Consts {
				if c := &g.Consts[i]; c.Name == name {
					return p.constant(f, &c.Value, depth+1)
				}
			}
		}
	}
	return "", false
}

package typeorm

import (
	"fmt"
	"strconv"

	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// This file reads one column member (rows TO-C1 to TO-C6) and renders its
// facts per dialect (render.go), then writes the kit column (finish).

// defValue is a literal default value: kind "null", "string", "number"
// (text: the literal's source text), "boolean" ("true"/"false") or "fn"
// (text: the SQL a `() => "<sql>"` arrow returns).
type defValue struct {
	kind, text string
}

// column is one column member as read.
type column struct {
	t          *table
	file, prop string
	line       int
	member     *typescript.ModelClassMember
	flags      typescript.CompilerFlags
	// deco is the typeorm column decorator; wrapper the TO-C6 wrapper's
	// name, wrapType and wrapDefault what it assigns.
	deco                  string
	wrapper               string
	wrapType, wrapDefault bool
	name, nameRow         string
	// unknownWhy makes the whole column unknown (its name is known).
	unknownWhy, unknownRow string
	// explicit is the literal type (positional or the type option).
	explicit         string
	typeWhy, typeRow string
	length           *int
	precision, scale *int
	nullable         *bool
	nullWhy          string
	hasDefault       bool
	def              defValue
	defWhy           string
	unique           *bool
	uniqueWhy        string
	primary          bool
	// strategy is a PrimaryGeneratedColumn's strategy; generated a
	// @Column({generated}) (generation not read).
	strategy  string
	generated bool
	// enum are the enum labels (TO-C5); isEnum says the column is an enum
	// column (type enum or simple-enum, or an enum option).
	isEnum   bool
	enum     []defValue
	enumWhy  string
	enumName string
	array    bool
	r        map[string]rendered
	// object is the TO-P1 (3) failure of a design type proven Object,
	// decided after the relation pass: a column that merges with a join
	// column has its type replaced before validation.
	object string
	// join links a join column the relation pass adds (TO-R1, TO-R2,
	// TO-R4) to its referenced column; mergedRef is the referenced column
	// of an explicit column a join column reuses, mergedRow
	// its relation's row.
	join      *joinInfo
	mergedRef *column
	mergedRow string
	// fkWhy makes the column's foreign-key list unknown; noFK says its
	// relation creates no foreign key (createForeignKeyConstraints:
	// false).
	fkWhy string
	noFK  bool
}

// rendered is a column's facts on one dialect; empty strings and nil
// pointers are unknown, with the reason beside them.
type rendered struct {
	typ, typWhy                 string
	length, precision, scale    *int
	ai                          *bool
	aiWhy                       string
	defKnown, present           bool
	text                        *string
	defWhy, textWhy, defaultRow string
	// defForm is the default's form (oracleDefaultForms): its verification
	// is the dialect's only when the oracle fixture compares that form on
	// that dialect (Revision 4 B1).
	defForm string
}

func (col *column) unknown(row, format string, args ...any) {
	if col.unknownWhy == "" {
		col.unknownWhy, col.unknownRow = fmt.Sprintf(format, args...), row
	}
}

// newColumn reads a column decorator call on a property; nil when the
// column's name is not known (the column set is then partial).
func (t *table) newColumn(file string, m *typescript.ModelClassMember, r decoratorRef) *column {
	c := t.c
	col := &column{t: t, file: file, prop: m.Name, line: m.Span.StartLine, member: m, deco: r.name, flags: c.d.in.Resolver.CompilerFlags(file)}
	switch r.name {
	case "CreateDateColumn", "UpdateDateColumn", "DeleteDateColumn", "VersionColumn":
		c.tag("TO-C3")
	default:
		c.tag("TO-C1")
	}
	var args []typescript.ModelExpr
	if r.call != nil {
		args = r.call.Args
	}
	var opts *typescript.ModelExpr
	switch r.name {
	case "PrimaryGeneratedColumn":
		col.primary, col.strategy = true, "increment"
		if len(args) > 0 && args[0].Kind != typescript.ModelObject {
			s, ok := stringLiteral(&args[0])
			if !ok {
				col.strategy = ""
			} else {
				col.strategy = s
			}
			args = args[1:]
		}
	case "Column", "PrimaryColumn":
		col.primary = r.name == "PrimaryColumn"
		if len(args) > 0 && args[0].Kind == typescript.ModelArrow {
			c.tag("D")
			c.unread(file, col.line, "the embedded column %s.%s is not read (row D)", t.e.Name, m.Name)
			t.partial("D", file, col.line, "%s: an embedded column (row D) adds columns that are not read", m.Name)
			return nil
		}
		if len(args) > 0 && args[0].Kind != typescript.ModelObject {
			s, ok := stringLiteral(&args[0])
			if !ok {
				col.typeWhy, col.typeRow = "the column type argument is not a string literal", "TO-C2"
			} else {
				col.explicit = s
			}
			args = args[1:]
		}
	}
	if len(args) > 1 {
		col.unknown("D", "the decorator's arguments are not read")
	} else if len(args) == 1 {
		if args[0].Kind != typescript.ModelObject {
			c.tag("TO-P2")
			t.partial("TO-P2", file, col.line, "%s: the column options are not an object literal; the column is not read", m.Name)
			return nil
		}
		opts = &args[0]
	}
	nameSet := false
	if opts != nil {
		for i := range opts.Entries {
			en := &opts.Entries[i]
			if en.Kind != typescript.ModelProperty && en.Kind != typescript.ModelShorthand {
				col.unknown("D", "a column option at line %d is not read", en.Span.StartLine)
				continue
			}
			if en.Key == "name" {
				n, ok := stringLiteral(&en.Value)
				if !ok || n == "" {
					c.tag(c.namingRow())
					t.partial(c.namingRow(), file, col.line, "%s: the column name is not a non-empty string literal", m.Name)
					return nil
				}
				col.name, nameSet = n, true
				continue
			}
			col.option(en.Key, &en.Value)
		}
	}
	if !nameSet {
		col.name = m.Name
		if c.g.Naming == "snake" {
			col.name = snakeCase(m.Name)
		}
	}
	col.nameRow = c.namingRow()
	c.tag(col.nameRow)
	if col.primary {
		t.primaries++
	}
	t.cols = append(t.cols, col)
	return col
}

// typeOptions are the options whose presence leaves the type unknown
// (TO-C2 refusals).
var typeOptions = map[string]bool{
	"unsigned": true, "zerofill": true, "width": true, "charset": true, "collation": true,
	"spatialFeatureType": true, "srid": true, "hstoreType": true, "generatedType": true, "asExpression": true,
}

// noFactOptions have no table fact (TO-C4; comment is not compared).
var noFactOptions = map[string]bool{
	"select": true, "insert": true, "update": true, "transformer": true, "comment": true,
	"primaryKeyConstraintName": true, "onUpdate": true,
}

// option reads one column option.
func (col *column) option(key string, v *typescript.ModelExpr) {
	intLit := func() (*int, bool) {
		if v.Kind == typescript.ModelNumber {
			if n, err := strconv.Atoi(v.Value); err == nil && n >= 0 {
				return &n, true
			}
		}
		if s, ok := stringLiteral(v); ok {
			if n, err := strconv.Atoi(s); err == nil && n >= 0 {
				return &n, true
			}
		}
		return nil, false
	}
	switch {
	case key == "type":
		s, ok := stringLiteral(v)
		switch {
		case ok:
			col.explicit = s
		case v.Kind == typescript.ModelCall:
			col.t.c.tag("TO-P2")
			col.typeWhy, col.typeRow = "the type is a project call ("+memberPath(v.Callee)+"(...)), not a literal", "TO-P2"
		default:
			col.typeWhy, col.typeRow = "the type is not a string literal", "TO-C2"
		}
	case key == "length":
		if n, ok := intLit(); ok {
			col.length = n
		} else {
			col.typeWhy, col.typeRow = "the length is not a literal", "TO-C2"
		}
	case key == "precision" || key == "scale":
		n, ok := intLit()
		switch {
		case !ok:
			col.typeWhy, col.typeRow = "the "+key+" is not a literal", "TO-C2"
		case key == "precision":
			col.precision = n
		default:
			col.scale = n
		}
	case key == "nullable":
		b, ok := boolLiteral(v)
		if ok {
			col.nullable = &b
		} else {
			col.nullWhy = "nullable is not a boolean literal"
		}
	case key == "default":
		col.hasDefault = true
		col.def, col.defWhy = col.defaultValue(v)
	case key == "unique":
		b, ok := boolLiteral(v)
		if ok {
			col.unique = &b
		} else {
			col.uniqueWhy = "unique is not a boolean literal"
		}
	case key == "primary":
		b, ok := boolLiteral(v)
		if !ok {
			col.unknown("TO-C1", "primary is not a boolean literal")
		} else if b {
			col.primary = true
		}
	case key == "enum":
		col.isEnum = true
		col.enum, col.enumWhy = col.enumValues(v)
	case key == "enumName":
		s, ok := stringLiteral(v)
		if !ok || s == "" {
			col.enumWhy = "enumName is not a string literal"
		} else {
			col.enumName = s
		}
	case key == "array":
		b, ok := boolLiteral(v)
		if !ok {
			col.typeWhy, col.typeRow = "array is not a boolean literal", "TO-C2"
		} else {
			col.array = b
		}
	case key == "generated":
		col.generated = true
	case typeOptions[key]:
		col.typeWhy, col.typeRow = "the "+key+" option is not read", "TO-C2"
	case noFactOptions[key]:
	default:
		col.t.c.tag("D")
		col.unknown("D", "the column option %s is not read (row D)", key)
	}
}

// defaultValue reads a default option (TO-C4): a string, number, boolean
// or null literal, a `() => "<sql>"` arrow with a literal body, or a member
// of an S8 enum.
func (col *column) defaultValue(v *typescript.ModelExpr) (defValue, string) {
	if s, ok := stringLiteral(v); ok {
		return defValue{kind: "string", text: s}, ""
	}
	switch v.Kind {
	case typescript.ModelNumber:
		return defValue{kind: "number", text: v.Value}, ""
	case typescript.ModelBoolean:
		return defValue{kind: "boolean", text: v.Value}, ""
	case typescript.ModelNull:
		return defValue{kind: "null"}, ""
	case typescript.ModelArrow:
		if len(v.Params) == 0 && !v.Async && v.Body != nil {
			if s, ok := stringLiteral(v.Body); ok {
				return defValue{kind: "fn", text: s}, ""
			}
		}
		return defValue{}, "the default function does not return a string literal"
	case typescript.ModelMember:
		if v.Object.Kind == typescript.ModelIdentifier {
			b := col.t.c.d.local(col.file, v.Object.Name, 0)
			if b.Enum != nil {
				values, ok := enumMembers(b.Enum)
				if !ok {
					return defValue{}, "the enum " + v.Object.Name + " has a member that is not read"
				}
				for i, m := range b.Enum.EnumMembers {
					if m.Name == v.Name {
						return values[i], ""
					}
				}
			}
		}
	}
	return defValue{}, "the default is not a literal or a member of an enum of the repository"
}

// enumValues reads an enum option (TO-C5): an array literal of strings and
// numbers, or an identifier of an S8 enum (not a const enum: it has no
// runtime object).
func (col *column) enumValues(v *typescript.ModelExpr) ([]defValue, string) {
	switch v.Kind {
	case typescript.ModelArray:
		var out []defValue
		for i := range v.Elements {
			el := &v.Elements[i]
			if s, ok := stringLiteral(el); ok {
				out = append(out, defValue{kind: "string", text: s})
			} else if el.Kind == typescript.ModelNumber {
				out = append(out, defValue{kind: "number", text: el.Value})
			} else {
				return nil, "an enum element is not a literal"
			}
		}
		return out, ""
	case typescript.ModelIdentifier:
		b := col.t.c.d.local(col.file, v.Name, 0)
		if b.Enum == nil || b.Enum.Const {
			return nil, "the enum " + v.Name + " does not resolve to an enum of the repository"
		}
		values, ok := enumMembers(b.Enum)
		if !ok {
			return nil, "the enum " + v.Name + " has a member that is not read"
		}
		return values, ""
	}
	return nil, "the enum option is not an array literal or an enum"
}

// boolFlag reads a compiler flag; ok is false when it is not proven.
func boolFlag(f typescript.CompilerFlags, name string) (value, ok bool) {
	if v, has := f.Values[name]; has {
		if v == nil {
			return false, false
		}
		return *v, true
	}
	if len(f.Unresolved) > 0 || f.Config == "" {
		return false, false
	}
	return false, true
}

// strictNullChecks reads strictNullChecks (strict implies it).
func strictNullChecks(f typescript.CompilerFlags) (value, ok bool) {
	if _, has := f.Values["strictNullChecks"]; has {
		return boolFlag(f, "strictNullChecks")
	}
	return boolFlag(f, "strict")
}

// designType is the design type tsc emits for the property's annotation
// when TO-C2 infers from it: exactly `string`, `number`, `boolean` or
// `Date`. object says the emitted type is proven Object
// (TO-P1 (3)).
func (col *column) designType() (design string, object string, why string) {
	emit, ok := boolFlag(col.flags, "emitDecoratorMetadata")
	if !ok || !emit {
		return "", "", "emitDecoratorMetadata is not proven true for " + col.file + " (TO-V3)"
	}
	ty := col.member.Type
	if ty == nil {
		return "", "the property has no type annotation (design type Object)", "the property has no type annotation"
	}
	switch {
	case ty.Kind == typescript.TypeKeyword && (ty.Name == "string" || ty.Name == "number" || ty.Name == "boolean"):
		return ty.Name, "", ""
	case ty.Kind == typescript.TypeReference && ty.Name == "Date" && ty.Arguments == 0:
		return "Date", "", ""
	case ty.Kind == typescript.TypeKeyword && (ty.Name == "any" || ty.Name == "unknown" || ty.Name == "object"):
		return "", "the annotation " + ty.Name + " emits the design type Object", "the annotation is " + ty.Name
	case ty.Kind == typescript.TypeUnion:
		nulls, others := 0, 0
		for _, m := range ty.Types {
			if m.Kind == typescript.TypeKeyword && (m.Name == "null" || m.Name == "undefined") {
				nulls++
			} else {
				others++
			}
		}
		if strict, ok := strictNullChecks(col.flags); ok && strict && nulls > 0 && others > 0 {
			return "", "a union with null or undefined emits the design type Object under strictNullChecks", "the annotation is a union"
		}
		return "", "", "the annotation is a union (the design type is not inferred)"
	}
	return "", "", "the annotation is not exactly string, number, boolean or Date"
}

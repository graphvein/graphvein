package routes

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// Schema readers of the Express shape rows: zod (SH-E20), Joi (SH-E21)
// and TypeScript types (SH-E22). Each maps exactly the forms its row lists
// and sends every other form to an unknown schema with a reason.

// validatorLib names a schema library a validator row reads.
type validatorLib string

const (
	libZod validatorLib = "zod"
	libJoi validatorLib = "joi"
)

// field is one schema with the presence its declaration gives.
type field struct {
	schema   routeshape.Schema
	presence routeshape.Presence
	// explicit marks a presence set by a Joi presence modifier (Joi's
	// presence option does not override it).
	explicit bool
	// emptyNull and insensitive are Joi modifiers whose effect depends on
	// the whole chain: empty(null) turns null into undefined (nullable
	// only when the key is not required), insensitive() changes valid().
	emptyNull, insensitive bool
}

// joiPrefs are what is known of the Joi preferences a schema is
// validated with. strict: convert is off (strict() on the schema or an
// ancestor); relaxed: options were passed (a Joi validate or celebrate
// options argument, not read), so presence defaults are not known.
type joiPrefs struct {
	strict, relaxed bool
}

func exUnknownSchema(reason string) routeshape.Schema {
	return routeshape.Schema{Type: routeshape.TypeUnknown, Reason: reason}
}

// maxSchemaRead bounds schema nesting and identifier hops while reading.
const maxSchemaRead = 16

// schemaReader reads validator schemas and TypeScript types in the
// context of one route (it collects the models types refer to).
type schemaReader struct {
	r      *resolver
	models *modelSet
	// next marks a Next.js App Router route or server action: the rows
	// reached are reported as their SH-N rows (nextRows).
	next bool
}

// tag reports a row reached while reading a schema.
func (sr *schemaReader) tag(row string) {
	if sr.next {
		row = nextRow(row)
	}
	tagShape(row)
}

// chainLink is one method call of a builder chain: name and arguments.
type chainLink struct {
	name string
	args []facts.RouteExpr
}

// builderChain splits `base(args).m1(a).m2(b)` into the innermost call
// (whose callee is not a method of a call) and the method calls on it,
// innermost first. It returns ok false for any other form.
func builderChain(e facts.RouteExpr) (base facts.RouteExpr, links []chainLink, ok bool) {
	for e.Kind == "call_expression" && e.Object != nil {
		callee := *e.Object
		if callee.Kind == "member_expression" && callee.Object != nil && callee.Object.Kind == "call_expression" {
			links = append([]chainLink{{name: callee.Name, args: e.Args}}, links...)
			e = *callee.Object
			continue
		}
		if callee.Kind == "member_expression" && callee.Object != nil && callee.Object.Kind == "identifier" {
			// The factory call (z.string()) or a method of a schema
			// const (UserSchema.extend()): the caller decides.
			return e, links, true
		}
		if callee.Kind == "identifier" {
			return e, links, true
		}
		return e, links, false
	}
	return e, links, e.Kind == "identifier" || e.Kind == "member_expression"
}

// library resolves the callee of a factory call to a validator library
// and the export path (`z.string` -> zod "string", `Joi.object` -> joi
// "object"). A schema const's method (`Base.extend`) returns "", "".
func (sr *schemaReader) library(file string, scope int, call facts.RouteExpr) (validatorLib, string) {
	if call.Object == nil {
		return "", ""
	}
	v := sr.r.resolve(file, scope, *call.Object, 0, nil)
	if v.kind != "import" {
		return "", ""
	}
	name := v.name
	for _, prefix := range []string{"default.", "z.", "Joi.", "*."} {
		name = strings.TrimPrefix(name, prefix)
	}
	switch v.file {
	case "zod", "zod/v4", "zod/v3":
		return libZod, name
	case "joi", "@hapi/joi":
		return libJoi, name
	case "celebrate":
		if strings.HasPrefix(v.name, "Joi.") {
			return libJoi, name
		}
	}
	return "", ""
}

// readValidator reads the schema named or written by e (a zod or Joi
// schema), following identifiers through bindings and imports. ok is false
// when e is not a schema of a known library.
func (sr *schemaReader) readValidator(file string, scope int, e facts.RouteExpr, prefs joiPrefs, depth int) (field, validatorLib, bool) {
	out, lib, ok := sr.readValidatorExpr(file, scope, e, prefs, depth)
	if ok {
		// A schema named by a module-level const is a model declaration
		// the route uses (row MT-5).
		if id := sr.schemaDecl(file, scope, e); id != "" && !containsString(out.schema.Decls, id) {
			out.schema.Decls = append(out.schema.Decls, id)
		}
	}
	return out, lib, ok
}

// schemaDecl follows an identifier the way deref does and returns the
// node identity of the last module-level const it passes through (the
// declaration of a named zod or Joi schema), or empty when none is
// proven: one variable or constant node of that name in its file.
func (sr *schemaReader) schemaDecl(file string, scope int, e facts.RouteExpr) string {
	r := sr.r
	declFile, declName := "", ""
	for depth := 0; e.Kind == "identifier" && depth <= maxDerefDepth; depth++ {
		b, at, ok := r.binding(file, scope, e.Name)
		if !ok {
			break
		}
		if b.Module != "" {
			x := r.modules.ImportBinding(file, b.Module, b.Export, "")
			if x.External || x.Namespace || x.File == "" || x.Name == "" {
				break
			}
			file, scope, e = x.File, 0, facts.RouteExpr{Kind: "identifier", Name: x.Name, Text: x.Name}
			continue
		}
		if b.Value.Kind == "" || b.Value.Kind == "function" || b.Value.Kind == "ambiguous" || b.Value.Kind == "parameter" {
			break
		}
		if at == 0 {
			declFile, declName = file, e.Name
		}
		scope, e = at, b.Value
	}
	if declName == "" {
		return ""
	}
	found := ""
	for _, n := range r.files[declFile].Nodes {
		if (n.Kind == "variable" || n.Kind == "constant") && n.QualifiedName == declName {
			if found != "" {
				return ""
			}
			found = n.Identity
		}
	}
	return found
}

func (sr *schemaReader) readValidatorExpr(file string, scope int, e facts.RouteExpr, prefs joiPrefs, depth int) (field, validatorLib, bool) {
	if depth > maxSchemaRead {
		return field{schema: exUnknownSchema("schema nested too deep")}, "", false
	}
	f, s, d := sr.r.deref(file, scope, e, 0)
	base, links, ok := builderChain(d)
	if !ok || base.Kind != "call_expression" {
		return field{}, "", false
	}
	lib, factory := sr.library(f, s, base)
	if lib == "" {
		// A method of another schema: Base.extend({...}) and the like.
		if base.Object != nil && base.Object.Kind == "member_expression" && base.Object.Object != nil {
			links = append([]chainLink{{name: base.Object.Name, args: base.Args}}, links...)
			inner, innerLib, ok := sr.readValidator(f, s, *base.Object.Object, joiStrict(prefs, links), depth+1)
			if !ok {
				return field{}, "", false
			}
			return sr.applyLinks(f, s, inner, innerLib, links, joiStrict(prefs, links), depth), innerLib, true
		}
		return field{}, "", false
	}
	var out field
	if lib == libZod {
		sr.tag("SH-E20")
	} else {
		sr.tag("SH-E21")
	}
	switch lib {
	case libZod:
		out = sr.zodFactory(f, s, factory, base.Args, prefs, depth)
	case libJoi:
		prefs = joiStrict(prefs, links)
		out = sr.joiFactory(f, s, factory, base.Args, prefs, depth)
	}
	return sr.applyLinks(f, s, out, lib, links, prefs, depth), lib, true
}

// joiStrict applies the strict() calls of a Joi chain to the inherited
// preferences: strict() and strict(true) turn convert off for the schema
// and its keys, strict(false) or an argument not read turns it on (Joi
// applies preferences to the schema and every key below it, whatever
// their place in the chain).
func joiStrict(prefs joiPrefs, links []chainLink) joiPrefs {
	for _, l := range links {
		if l.name != "strict" {
			continue
		}
		prefs.strict = len(l.args) == 0 || l.args[0].Kind == "true"
	}
	return prefs
}

func (sr *schemaReader) zodFactory(file string, scope int, name string, args []facts.RouteExpr, prefs joiPrefs, depth int) field {
	f := field{presence: routeshape.Required}
	switch name {
	case "string":
		f.schema = routeshape.Schema{Type: routeshape.TypeString}
	case "number":
		f.schema = routeshape.Schema{Type: routeshape.TypeNumber}
	case "int":
		f.schema = routeshape.Schema{Type: routeshape.TypeInteger}
	case "boolean":
		f.schema = routeshape.Schema{Type: routeshape.TypeBoolean}
	case "null":
		f.schema = routeshape.Schema{Type: routeshape.TypeNull}
	case "literal":
		if len(args) == 1 {
			if v, ok := exLiteralValue(args[0]); ok {
				f.schema = routeshape.Schema{Type: routeshape.TypeEnum, Enum: []any{v}}
				return f
			}
		}
		f.schema = exUnknownSchema("zod literal of a value not read")
	case "enum":
		if len(args) >= 1 && args[0].Kind == "array" {
			// routeExpr keeps no array items: the values are not read.
		}
		f.schema = exUnknownSchema("zod enum values not read")
	case "array":
		if len(args) == 1 {
			inner, _, ok := sr.readValidator(file, scope, args[0], prefs, depth+1)
			items := inner.schema
			if !ok {
				items = exUnknownSchema("array item schema not read")
			}
			f.schema = routeshape.Schema{Type: routeshape.TypeArray, Items: &items}
			return f
		}
		f.schema = exUnknownSchema("zod array without an item schema")
	case "object", "strictObject", "looseObject":
		if len(args) >= 1 {
			f.schema = sr.objectFields(file, scope, args[0], libZod, prefs, depth)
			if f.schema.Type == routeshape.TypeObject {
				switch name {
				case "strictObject":
					f.schema.Additional = routeshape.AdditionalForbidden
				default:
					// zod strips unknown keys: the request may carry any.
					f.schema.Additional = routeshape.AdditionalAllowed
				}
			}
			return f
		}
		f.schema = exUnknownSchema("zod object without a shape")
	default:
		f.schema = exUnknownSchema("zod " + name + " not read")
		f.presence = routeshape.PresenceUnknown
	}
	return f
}

func (sr *schemaReader) joiFactory(file string, scope int, name string, args []facts.RouteExpr, prefs joiPrefs, depth int) field {
	// Joi keys are optional unless required() (presence 'optional').
	f := field{presence: routeshape.Optional}
	switch name {
	case "string":
		f.schema = routeshape.Schema{Type: routeshape.TypeString}
	case "number", "boolean", "bool":
		// Joi validates with convert on by default: number() accepts
		// "5", boolean() accepts "true" (checked with Joi 17.13.3). The
		// type is claimed only with convert proven off (strict()).
		switch {
		case !prefs.strict:
			what := "numbers"
			if name != "number" {
				what = "booleans"
			}
			f.schema = exUnknownSchema("Joi converts strings to " + what + " unless strict(): type not claimed")
		case name == "number":
			f.schema = routeshape.Schema{Type: routeshape.TypeNumber}
		default:
			f.schema = routeshape.Schema{Type: routeshape.TypeBoolean}
		}
	case "array":
		items := exUnknownSchema("array items not declared")
		f.schema = routeshape.Schema{Type: routeshape.TypeArray, Items: &items}
	case "object":
		if len(args) >= 1 {
			f.schema = sr.objectFields(file, scope, args[0], libJoi, prefs, depth)
			if f.schema.Type == routeshape.TypeObject {
				f.schema.Additional = routeshape.AdditionalForbidden
			}
			return f
		}
		// Joi.object() without keys accepts any object.
		f.schema = routeshape.Schema{Type: routeshape.TypeObject, Additional: routeshape.AdditionalAllowed, Fields: nil}
	default:
		f.schema = exUnknownSchema("Joi " + name + " not read")
		f.presence = routeshape.PresenceUnknown
	}
	return f
}

// objectFields reads an object literal of field schemas.
func (sr *schemaReader) objectFields(file string, scope int, e facts.RouteExpr, lib validatorLib, prefs joiPrefs, depth int) routeshape.Schema {
	f, s, d := sr.r.deref(file, scope, e, 0)
	if d.Kind != "object" || d.Spread {
		return exUnknownSchema("object shape not read (not an object literal, or a spread)")
	}
	names := make([]string, 0, len(d.Properties))
	for k := range d.Properties {
		names = append(names, k)
	}
	sort.Strings(names)
	out := routeshape.Schema{Type: routeshape.TypeObject, Additional: routeshape.AdditionalUnknown}
	for _, k := range names {
		if strings.HasPrefix(k, "[") {
			return exUnknownSchema("computed key in an object shape")
		}
		v, gotLib, ok := sr.readValidator(f, s, d.Properties[k], prefs, depth+1)
		if !ok || gotLib != lib {
			v = field{schema: exUnknownSchema("field schema not read"), presence: routeshape.PresenceUnknown}
		}
		out.Fields = append(out.Fields, routeshape.Field{Name: k, Presence: v.presence, Schema: v.schema})
	}
	return out
}

// Modifiers that keep a schema's type and presence (validation only).
var zodKeep = map[string]bool{
	"describe": true, "min": true, "max": true, "length": true, "email": true, "url": true, "uuid": true,
	"cuid": true, "cuid2": true, "regex": true, "trim": true, "toLowerCase": true, "toUpperCase": true,
	"startsWith": true, "endsWith": true, "positive": true, "negative": true, "nonnegative": true,
	"nonpositive": true, "finite": true, "nonempty": true, "datetime": true, "refine": true,
	"superRefine": true, "meta": true, "includes": true, "gt": true, "gte": true, "lt": true, "lte": true,
	"multipleOf": true, "ulid": true, "emoji": true,
}

var joiKeep = map[string]bool{
	"description": true, "label": true, "example": true, "meta": true, "notes": true, "tags": true,
	"trim": true, "lowercase": true, "uppercase": true, "min": true, "max": true, "length": true,
	"email": true, "uri": true, "uuid": true, "guid": true, "pattern": true, "regex": true,
	"positive": true, "negative": true, "greater": true, "less": true, "precision": true,
	"alphanum": true, "token": true, "hex": true, "messages": true, "message": true,
	// strict() is read before the chain (joiStrict).
	"strict": true,
}

// joiTypeChecks are Joi modifiers that check or convert values without
// changing presence; on a schema whose type is not claimed they keep the
// presence.
var joiTypeChecks = map[string]bool{
	"integer": true, "multiple": true, "port": true, "sign": true, "valid": true, "equal": true,
	"allow": true, "invalid": true, "disallow": true, "not": true, "truthy": true, "falsy": true,
	"sensitive": true, "insensitive": true, "unsafe": true, "empty": true,
}

// applyLinks applies the method calls of a builder chain to a schema.
func (sr *schemaReader) applyLinks(file string, scope int, f field, lib validatorLib, links []chainLink, prefs joiPrefs, depth int) field {
	for _, l := range links {
		if lib == libJoi && joiPresence(&f, l.name) {
			continue
		}
		if f.schema.Type == routeshape.TypeUnknown {
			// Once unknown, only presence modifiers still read.
			switch {
			case lib == libZod && (l.name == "optional" || l.name == "default" || l.name == "nullish") && f.presence != routeshape.PresenceUnknown:
				f.presence = routeshape.Optional
			case (lib == libZod && zodKeep[l.name]) || (lib == libJoi && (joiKeep[l.name] || joiTypeChecks[l.name])):
			default:
				f.presence = routeshape.PresenceUnknown
			}
			continue
		}
		switch lib {
		case libZod:
			f = sr.zodLink(file, scope, f, l, prefs, depth)
		case libJoi:
			f = sr.joiLink(file, scope, f, l, prefs, depth)
		}
	}
	if lib == libJoi && f.schema.Type != routeshape.TypeUnknown {
		f = joiSettle(f, prefs)
	}
	return f
}

// joiPresence applies a Joi presence modifier: the last one of the chain
// wins (required/exist, optional); forbidden() and a presence already
// unknown stay unknown; default() keeps the presence.
func joiPresence(f *field, name string) bool {
	switch name {
	case "required", "exist", "exists", "optional":
		if f.presence != routeshape.PresenceUnknown {
			f.presence = routeshape.Optional
			if name != "optional" {
				f.presence = routeshape.Required
			}
			f.explicit = true
		}
	case "forbidden", "strip":
		f.presence = routeshape.PresenceUnknown
	case "default":
	default:
		return false
	}
	return true
}

// joiSettle applies the Joi modifiers whose effect depends on the whole
// chain: insensitive() makes valid() values unknown; empty(null) makes
// null acceptable only for a key that is not required (null becomes
// undefined), and unknown when that presence is not known.
func joiSettle(f field, prefs joiPrefs) field {
	if f.insensitive && f.schema.Type == routeshape.TypeEnum {
		return field{schema: exUnknownSchema("Joi valid() with insensitive(): values compare without case"), presence: f.presence, explicit: f.explicit}
	}
	if f.emptyNull {
		switch {
		case f.presence == routeshape.Required:
			f.schema.Nullable = false
		case f.presence == routeshape.Optional && (f.explicit || !prefs.relaxed):
			f.schema.Nullable = true
		default:
			return field{schema: exUnknownSchema("Joi empty(null) on a key whose presence is not read"), presence: f.presence, explicit: f.explicit}
		}
	}
	return f
}

func (sr *schemaReader) zodLink(file string, scope int, f field, l chainLink, prefs joiPrefs, depth int) field {
	switch {
	case zodKeep[l.name]:
	case l.name == "optional" || l.name == "default":
		if f.presence != routeshape.PresenceUnknown {
			f.presence = routeshape.Optional
		}
	case l.name == "nullable":
		f.schema.Nullable = true
	case l.name == "nullish":
		f.schema.Nullable = true
		if f.presence != routeshape.PresenceUnknown {
			f.presence = routeshape.Optional
		}
	case l.name == "int" && f.schema.Type == routeshape.TypeNumber:
		f.schema.Type = routeshape.TypeInteger
	case l.name == "strict" && f.schema.Type == routeshape.TypeObject:
		f.schema.Additional = routeshape.AdditionalForbidden
	case (l.name == "passthrough" || l.name == "strip" || l.name == "loose") && f.schema.Type == routeshape.TypeObject:
		f.schema.Additional = routeshape.AdditionalAllowed
	case l.name == "array":
		items := f.schema
		f.schema = routeshape.Schema{Type: routeshape.TypeArray, Items: &items}
		f.presence = routeshape.Required
	case l.name == "extend" && f.schema.Type == routeshape.TypeObject && len(l.args) == 1:
		extra := sr.objectFields(file, scope, l.args[0], libZod, prefs, depth)
		if extra.Type != routeshape.TypeObject {
			return field{schema: exUnknownSchema("zod extend with a shape not read"), presence: f.presence}
		}
		byName := map[string]int{}
		for i, x := range f.schema.Fields {
			byName[x.Name] = i
		}
		fields := append([]routeshape.Field(nil), f.schema.Fields...)
		for _, x := range extra.Fields {
			if i, ok := byName[x.Name]; ok {
				fields[i] = x
			} else {
				fields = append(fields, x)
			}
		}
		sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
		f.schema.Fields = fields
	case l.name == "partial" && f.schema.Type == routeshape.TypeObject && len(l.args) == 0:
		fields := append([]routeshape.Field(nil), f.schema.Fields...)
		for i := range fields {
			if fields[i].Presence == routeshape.Required {
				fields[i].Presence = routeshape.Optional
			}
		}
		f.schema.Fields = fields
	default:
		return field{schema: exUnknownSchema("zod ." + l.name + "() not read"), presence: routeshape.PresenceUnknown}
	}
	return f
}

func (sr *schemaReader) joiLink(file string, scope int, f field, l chainLink, prefs joiPrefs, depth int) field {
	switch {
	case joiKeep[l.name]:
	case l.name == "insensitive":
		f.insensitive = true
	case l.name == "integer" && f.schema.Type == routeshape.TypeNumber:
		f.schema.Type = routeshape.TypeInteger
	case l.name == "items" && f.schema.Type == routeshape.TypeArray && len(l.args) == 1:
		inner, lib, ok := sr.readValidator(file, scope, l.args[0], prefs, depth+1)
		items := inner.schema
		if !ok || lib != libJoi {
			items = exUnknownSchema("array item schema not read")
		}
		f.schema.Items = &items
	case l.name == "keys" && f.schema.Type == routeshape.TypeObject && len(f.schema.Fields) == 0 && len(l.args) == 1:
		keys := sr.objectFields(file, scope, l.args[0], libJoi, prefs, depth)
		if keys.Type != routeshape.TypeObject {
			return field{schema: keys, presence: f.presence, explicit: f.explicit}
		}
		keys.Additional = routeshape.AdditionalForbidden
		f.schema = keys
	case l.name == "unknown" && f.schema.Type == routeshape.TypeObject && (len(l.args) == 0 || l.args[0].Kind == "true"):
		f.schema.Additional = routeshape.AdditionalAllowed
	case l.name == "allow" && allNull(l.args):
		f.schema.Nullable = true
	case l.name == "empty" && allNull(l.args):
		f.emptyNull = true
	case (l.name == "allow" || l.name == "empty") && f.schema.Type == routeshape.TypeString && allEmptyString(l.args):
		// '' stays within type string (allowed, or turned into undefined).
	case l.name == "valid" || l.name == "equal":
		var values []any
		for _, a := range l.args {
			v, ok := exLiteralValue(a)
			if !ok {
				return field{schema: exUnknownSchema("Joi valid() of a value not read"), presence: f.presence, explicit: f.explicit}
			}
			values = append(values, v)
		}
		if len(values) == 0 {
			return field{schema: exUnknownSchema("Joi valid() without values"), presence: f.presence, explicit: f.explicit}
		}
		f.schema = routeshape.Schema{Type: routeshape.TypeEnum, Enum: values, Nullable: f.schema.Nullable}
	case l.name == "empty":
		return field{schema: exUnknownSchema("Joi empty() of a value other than null or ''"), presence: f.presence, explicit: f.explicit}
	default:
		return field{schema: exUnknownSchema("Joi ." + l.name + "() not read"), presence: routeshape.PresenceUnknown}
	}
	return f
}

func allNull(args []facts.RouteExpr) bool {
	for _, a := range args {
		if a.Kind != "null" {
			return false
		}
	}
	return len(args) > 0
}

func allEmptyString(args []facts.RouteExpr) bool {
	for _, a := range args {
		if a.Kind != "literal" || a.Name != "" {
			return false
		}
	}
	return len(args) > 0
}

// exLiteralValue reads a string, number, boolean or null literal.
func exLiteralValue(e facts.RouteExpr) (any, bool) {
	switch e.Kind {
	case "literal":
		return e.Name, true
	case "number":
		n := e.Name
		if n == "" {
			n = e.Text
		}
		v, err := strconv.ParseFloat(n, 64)
		return v, err == nil
	case "true":
		return true, true
	case "false":
		return false, true
	case "null":
		return nil, true
	}
	return nil, false
}

// modelSet collects the named TypeScript types a route's schemas refer to.
type modelSet struct {
	byKey  map[string]string
	models []routeshape.Model
	taken  map[string]bool
}

func newModelSet() *modelSet {
	return &modelSet{byKey: map[string]string{}, taken: map[string]bool{}}
}

// tsSchema converts a TypeScript type to a schema (row SH-E22). Named types
// declared in the repository become models of the given kind.
func (sr *schemaReader) tsSchema(file string, t facts.ShapeType, kind routeshape.Kind, depth int) routeshape.Schema {
	if depth > maxSchemaRead {
		return exUnknownSchema("type nested too deep")
	}
	switch t.Kind {
	case "string":
		return routeshape.Schema{Type: routeshape.TypeString}
	case "number":
		return routeshape.Schema{Type: routeshape.TypeNumber}
	case "boolean":
		return routeshape.Schema{Type: routeshape.TypeBoolean}
	case "null":
		return routeshape.Schema{Type: routeshape.TypeNull}
	case "literal":
		v, ok := exLiteralValue(literalExpr(t.Name))
		if !ok {
			return exUnknownSchema("literal type not read")
		}
		return routeshape.Schema{Type: routeshape.TypeEnum, Enum: []any{v}}
	case "array":
		items := exUnknownSchema("array item type not read")
		if len(t.Args) == 1 {
			items = sr.tsSchema(file, t.Args[0], kind, depth+1)
		}
		return routeshape.Schema{Type: routeshape.TypeArray, Items: &items}
	case "object":
		out := routeshape.Schema{Type: routeshape.TypeObject, Additional: routeshape.AdditionalUnknown}
		seen := map[string]bool{}
		for _, p := range t.Props {
			if seen[p.Name] {
				return exUnknownSchema("object type with a repeated property")
			}
			seen[p.Name] = true
			presence := routeshape.PresenceUnknown
			if p.Optional || hasUndefined(p.Type) {
				// `x: T | undefined`: JSON drops an undefined property,
				// and TypeScript accepts it missing only with `?`; the
				// key may be absent either way (never nullable).
				presence = routeshape.Optional
			}
			out.Fields = append(out.Fields, routeshape.Field{Name: p.Name, Presence: presence, Schema: sr.tsSchema(file, p.Type, kind, depth+1)})
		}
		sort.Slice(out.Fields, func(i, j int) bool { return out.Fields[i].Name < out.Fields[j].Name })
		if t.Open {
			return exUnknownSchema("object type with members other than properties, or extending another type")
		}
		return out
	case "union":
		var rest []facts.ShapeType
		nullable := false
		for _, a := range t.Args {
			switch a.Kind {
			case "null":
				nullable = true
				continue
			case "undefined":
				// undefined makes a property optional, never nullable.
				continue
			}
			rest = append(rest, a)
		}
		if len(rest) == 1 {
			s := sr.tsSchema(file, rest[0], kind, depth+1)
			if s.Type != routeshape.TypeUnknown {
				s.Nullable = s.Nullable || nullable
			}
			return s
		}
		var values []any
		for _, a := range rest {
			if a.Kind != "literal" {
				return exUnknownSchema("union type not read")
			}
			v, ok := exLiteralValue(literalExpr(a.Name))
			if !ok {
				return exUnknownSchema("union type not read")
			}
			values = append(values, v)
		}
		if len(values) == 0 {
			return exUnknownSchema("union type not read")
		}
		return routeshape.Schema{Type: routeshape.TypeEnum, Enum: values, Nullable: nullable}
	case "name":
		if (t.Name == "Array" || t.Name == "ReadonlyArray") && len(t.Args) == 1 {
			items := sr.tsSchema(file, t.Args[0], kind, depth+1)
			return routeshape.Schema{Type: routeshape.TypeArray, Items: &items}
		}
		if len(t.Args) > 0 {
			return exUnknownSchema("generic type " + t.Name + " not read")
		}
		return sr.namedType(file, t.Name, kind, depth)
	}
	return exUnknownSchema("type " + orOther(t.Kind) + " not read")
}

func orOther(k string) string {
	if k == "" {
		return "other"
	}
	return k
}

// literalExpr turns a literal type's text into an expression exLiteralValue
// reads.
func literalExpr(text string) facts.RouteExpr {
	switch {
	case text == "true" || text == "false":
		return facts.RouteExpr{Kind: text}
	case len(text) >= 2 && (text[0] == '"' || text[0] == '\''):
		return facts.RouteExpr{Kind: "literal", Name: text[1 : len(text)-1]}
	}
	return facts.RouteExpr{Kind: "number", Name: text}
}

// namedType resolves a type name declared in file or imported into it to
// a repository interface or type alias and returns a ref to its model.
func (sr *schemaReader) namedType(file, name string, kind routeshape.Kind, depth int) routeshape.Schema {
	declFile, declName, t, ok := sr.lookupType(file, name)
	if !ok {
		return exUnknownSchema("type " + name + " not found in the repository")
	}
	key := declFile + "\x00" + declName + "\x00" + string(kind)
	if model, done := sr.models.byKey[key]; done {
		return routeshape.Schema{Type: routeshape.TypeRef, Ref: model}
	}
	model := declName
	for i := 2; sr.models.taken[model]; i++ {
		model = fmt.Sprintf("%s_%d", declName, i)
	}
	sr.tag("SH-E22")
	sr.models.taken[model] = true
	sr.models.byKey[key] = model
	at := len(sr.models.models)
	line := 1
	identity := ""
	for _, n := range sr.r.files[declFile].Nodes {
		if (n.Kind == "interface" || n.Kind == "type_alias") && n.QualifiedName == declName && n.Span != nil {
			line = n.Span.StartLine
			identity = n.Identity
			break
		}
	}
	sr.models.models = append(sr.models.models, routeshape.Model{Name: model, Provenance: routeshape.Provenance{Kind: kind, Rule: "SH-E22", File: declFile, Line: line, Confidence: 1}})
	schema := sr.tsSchema(declFile, t, kind, depth+1)
	if identity != "" {
		// The declaration the model was read from (row MT-5).
		schema.Decls = append(schema.Decls, identity)
	}
	sr.models.models[at].Schema = schema
	return routeshape.Schema{Type: routeshape.TypeRef, Ref: model}
}

// lookupType finds the declaration of a type name: a top-level interface
// or type alias of file, or one an import of file names.
func (sr *schemaReader) lookupType(file, name string) (string, string, facts.ShapeType, bool) {
	for hop := 0; hop < 4; hop++ {
		if syn := sr.r.files[file].Shape; syn != nil {
			if t, ok := syn.Types[name]; ok {
				if t.Kind == "other" && t.Name == "declared twice" {
					return "", "", facts.ShapeType{}, false
				}
				return file, name, t, true
			}
		}
		data := sr.r.files[file].Routes
		if data == nil {
			return "", "", facts.ShapeType{}, false
		}
		if strings.Contains(name, ".") || len(data.Scopes) == 0 {
			return "", "", facts.ShapeType{}, false
		}
		b, ok := data.Scopes[0].Bindings[name]
		if !ok || b.Module == "" {
			return "", "", facts.ShapeType{}, false
		}
		x := sr.r.modules.ImportBinding(file, b.Module, b.Export, "")
		if x.External || x.Namespace || x.File == "" || x.Name == "" {
			return "", "", facts.ShapeType{}, false
		}
		file, name = x.File, x.Name
	}
	return "", "", facts.ShapeType{}, false
}

// hasUndefined reports a union type with undefined among its members.
func hasUndefined(t facts.ShapeType) bool {
	if t.Kind != "union" {
		return false
	}
	for _, a := range t.Args {
		if a.Kind == "undefined" {
			return true
		}
	}
	return false
}

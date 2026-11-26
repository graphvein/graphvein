package routes

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// This file resolves Rust types written in handler signatures and serde
// declarations, and builds the schema serde reads or writes for them (rows
// SH-X16..SH-X20 of docs/contract/claims/route-shape-axum.md).

// axumTypeDecl is a cached type declaration with its file.
type axumTypeDecl struct {
	file string
	decl *facts.RustTypeDecl
}

// tref is a resolved type: a local declaration with its arguments, an
// external or standard type by crate and last segment, a tuple, an array,
// `impl Trait`, unit, or something not read (kind "other", with why).
type tref struct {
	// kind is "local", "ext", "tuple", "array", "impl", "unit", "generic"
	// or "other".
	kind string
	// crate is the external crate ("std" for primitives and the prelude);
	// name its last path segment, or the trait's for impl.
	crate, name, path string
	decl              *axumTypeDecl
	args              []*tref
	text              string
	why               string
}

func (t *tref) is(crates, name string) bool {
	return t != nil && t.kind == "ext" && t.name == name && facts.Builtin(t.crate, crates)
}

// std reports a standard library (or primitive or prelude) type by name.
func (t *tref) std(names string) bool {
	return t != nil && t.kind == "ext" && t.crate == "std" && facts.Builtin(t.name, names)
}

func (t *tref) arg(i int) *tref {
	if t == nil || i >= len(t.args) {
		return nil
	}
	return t.args[i]
}

// preludeNames are the names a Rust file uses without an import: the
// primitives and the std prelude types the shape reads.
const preludeNames = "bool i8 i16 i32 i64 i128 isize u8 u16 u32 u64 u128 usize f32 f64 char str String Vec Option Box Result"

// stdCrates are the crates whose items count as the standard library.
const stdCrates = "std core alloc"

// resolveType resolves t written in file's scope, with subst giving the
// generic parameters of the declaration being read.
func (s *axumShaper) resolveType(t facts.RustType, file string, scope int, subst map[string]*tref, depth int) *tref {
	if depth > 24 {
		return &tref{kind: "other", text: t.Text, why: "type nested too deeply"}
	}
	switch t.Kind {
	case "ref":
		if len(t.Args) == 1 {
			return s.resolveType(t.Args[0], file, scope, subst, depth+1)
		}
	case "tuple", "array":
		out := &tref{kind: t.Kind, text: t.Text}
		for _, a := range t.Args {
			out.args = append(out.args, s.resolveType(a, file, scope, subst, depth+1))
		}
		return out
	case "unit":
		return &tref{kind: "unit", text: "()"}
	case "impl":
		return &tref{kind: "impl", name: lastSegment(t.Name), text: t.Text}
	case "path":
		return s.resolvePath(t, file, scope, subst, depth)
	}
	return &tref{kind: "other", text: t.Text, why: "type form not read"}
}

func lastSegment(p string) string {
	if i := strings.LastIndex(p, "::"); i >= 0 {
		return p[i+2:]
	}
	return p
}

func (s *axumShaper) resolvePath(t facts.RustType, file string, scope int, subst map[string]*tref, depth int) *tref {
	args := make([]*tref, 0, len(t.Args))
	for _, a := range t.Args {
		args = append(args, s.resolveType(a, file, scope, subst, depth+1))
	}
	if !strings.Contains(t.Name, "::") {
		if v, ok := subst[t.Name]; ok {
			if v == nil {
				return &tref{kind: "generic", text: t.Name, why: "generic parameter " + t.Name + " is not instantiated"}
			}
			return v
		}
	}
	b := s.modules.Resolve(file, scope, t.Name)
	if b.Identity == "" && !strings.Contains(t.Name, "::") && facts.Builtin(t.Name, preludeNames) {
		// A primitive or prelude name no repository item shadows.
		return &tref{kind: "ext", crate: "std", name: t.Name, path: t.Name, args: args, text: t.Text}
	}
	switch {
	case b.Ambiguous:
		return &tref{kind: "other", text: t.Text, why: "type " + t.Text + " is ambiguous"}
	case b.Identity != "":
		d := s.types[b.Identity]
		if d == nil {
			return &tref{kind: "other", text: t.Text, why: "declaration of " + t.Text + " not read"}
		}
		if d.decl.Kind == "alias" {
			if d.decl.Alias == nil {
				return &tref{kind: "other", text: t.Text, why: "alias " + t.Text + " not read"}
			}
			inner := map[string]*tref{}
			for i, g := range d.decl.Generics {
				switch {
				case i < len(args):
					inner[g] = args[i]
				case i < len(d.decl.Defaults) && d.decl.Defaults[i].Kind != "":
					inner[g] = s.resolveType(d.decl.Defaults[i], d.file, d.decl.Scope, nil, depth+1)
				default:
					inner[g] = nil
				}
			}
			return s.resolveType(*d.decl.Alias, d.file, d.decl.Scope, inner, depth+1)
		}
		return &tref{kind: "local", name: d.decl.Name, decl: d, args: args, text: t.Text}
	case b.External != "":
		crate := b.External
		if facts.Builtin(crate, stdCrates) {
			crate = "std"
		}
		return &tref{kind: "ext", crate: crate, name: lastSegment(b.Name), path: b.Name, args: args, text: t.Text}
	case !strings.Contains(t.Name, "::") && facts.Builtin(t.Name, preludeNames):
		return &tref{kind: "ext", crate: "std", name: t.Name, path: t.Name, args: args, text: t.Text}
	}
	return &tref{kind: "other", text: t.Text, why: "type " + t.Text + " is not resolved"}
}

// view is the direction serde runs in: deserializing a request (de),
// deserializing URL-encoded pairs (form: query strings and form bodies),
// or serializing a response (ser).
type view int

const (
	viewDe view = iota
	viewForm
	viewSer
	// viewPath is axum's own path deserializer: scalars, including 128-bit
	// integers, and no sequences or maps.
	viewPath
)

// urlView reports a view of URL data (path segments, query pairs, form
// pairs): no sequences or maps, and no null.
func (c *schemaCtx) urlView() bool { return c.view == viewForm || c.view == viewPath }

// schemaCtx carries the declarations being read (recursion guard) and the
// nesting depth.
type schemaCtx struct {
	view    view
	visit   map[string]bool
	depth   int
	partial bool
}

// schema returns the schema serde reads or writes for t (rows SH-X16 to
// SH-X20).
func (s *axumShaper) schema(t *tref, c *schemaCtx) routeshape.Schema {
	if c.depth > routeshape.MaxSchemaDepth-2 {
		return unknownSchema("nested too deeply")
	}
	c.depth++
	defer func() { c.depth-- }()
	switch t.kind {
	case "unit":
		return routeshape.Schema{Type: routeshape.TypeNull}
	case "generic", "other":
		return unknownSchema(t.why)
	case "impl":
		return unknownSchema("impl " + t.name + " is opaque")
	case "tuple":
		return unknownSchema("tuple " + t.text + " (a fixed-length array) is not modelled")
	case "array":
		items := s.schema(t.arg(0), c)
		return routeshape.Schema{Type: routeshape.TypeArray, Items: &items}
	case "ext":
		return s.extSchema(t, c)
	case "local":
		out := s.localSchema(t, c)
		// The resolved declaration is used whatever its schema reads as
		// (row MT-7).
		if id := t.decl.decl.Identity; id != "" && !containsString(out.Decls, id) {
			out.Decls = append(out.Decls, id)
		}
		return out
	}
	return unknownSchema("type form not read")
}

func (s *axumShaper) extSchema(t *tref, c *schemaCtx) routeshape.Schema {
	if t.crate == "std" {
		switch {
		case t.std("bool"):
			return routeshape.Schema{Type: routeshape.TypeBoolean}
		case t.std("i8 i16 i32 i64 i128 isize u8 u16 u32 u64 u128 usize"):
			return routeshape.Schema{Type: routeshape.TypeInteger}
		case t.std("f32 f64"):
			// serde_json writes NaN and the infinities as null.
			return routeshape.Schema{Type: routeshape.TypeNumber, Nullable: c.view == viewSer}
		case t.std("String str char"):
			return routeshape.Schema{Type: routeshape.TypeString}
		case t.std("Option") && len(t.args) == 1:
			inner := s.schema(t.args[0], c)
			if inner.Type == routeshape.TypeUnknown {
				return inner
			}
			if !c.urlView() {
				// URL-encoded data has no null: a missing pair is None.
				inner.Nullable = true
			}
			return inner
		case t.std("Box Rc Arc") && len(t.args) == 1:
			return s.schema(t.args[0], c)
		case t.std("Cow") && len(t.args) == 1:
			return s.schema(t.args[0], c)
		case t.std("Vec VecDeque LinkedList HashSet BTreeSet BinaryHeap") && len(t.args) == 1:
			if c.urlView() {
				return unknownSchema("a sequence in URL-encoded data is not read")
			}
			items := s.schema(t.args[0], c)
			return routeshape.Schema{Type: routeshape.TypeArray, Items: &items}
		case t.std("HashMap BTreeMap") && len(t.args) == 2:
			if c.urlView() {
				return unknownSchema("a map in URL-encoded data is not read")
			}
			return routeshape.Schema{Type: routeshape.TypeObject, Additional: routeshape.AdditionalAllowed}
		}
		return unknownSchema("standard type " + t.text + " is not modelled")
	}
	switch {
	case t.is("uuid", "Uuid"):
		return routeshape.Schema{Type: routeshape.TypeString}
	case t.crate == "chrono" && facts.Builtin(t.name, "DateTime NaiveDate NaiveDateTime NaiveTime"):
		return routeshape.Schema{Type: routeshape.TypeString}
	case t.is("serde_json", "Value"):
		return unknownSchema("any JSON value (serde_json::Value)")
	}
	return unknownSchema(fmt.Sprintf("type %s of crate %s is not modelled", t.text, t.crate))
}

// serdeAttr returns the value of a serde argument and whether it is present.
func serdeAttr(attrs []facts.RustAttr, name string) (facts.RustAttr, bool) {
	for _, a := range attrs {
		if a.Name == name {
			return a, true
		}
	}
	return facts.RustAttr{}, false
}

// derives reports whether a declaration derives a serde trait for the view.
func derives(d *facts.RustTypeDecl, v view) bool {
	want := "Deserialize"
	if v == viewSer {
		want = "Serialize"
	}
	for _, p := range d.Derives {
		if lastSegment(p) == want {
			return true
		}
	}
	return false
}

// containerSafe lists the container attributes the schema models; any
// other one sends the type to unknown.
const containerSafe = "rename_all deny_unknown_fields default rename bound crate expecting"

// fieldSafe lists the field attributes the schema models.
const fieldSafe = "rename alias default skip skip_serializing skip_deserializing skip_serializing_if with serialize_with deserialize_with flatten borrow getter bound"

func (s *axumShaper) localSchema(t *tref, c *schemaCtx) routeshape.Schema {
	d := t.decl.decl
	if !derives(d, c.view) {
		trait := "Deserialize"
		if c.view == viewSer {
			trait = "Serialize"
		}
		return unknownSchema(fmt.Sprintf("%s does not derive serde %s (custom or no implementation)", d.Name, trait))
	}
	key := t.decl.decl.Identity + "<" + t.text + ">"
	if c.visit[key] {
		return unknownSchema("recursive type " + d.Name)
	}
	c.visit[key] = true
	defer delete(c.visit, key)
	for _, a := range d.Serde {
		if a.Nested || !facts.Builtin(a.Name, containerSafe) {
			return unknownSchema(fmt.Sprintf("serde attribute %s on %s is not modelled", a.Name, d.Name))
		}
	}
	if a, ok := unreadAttr(d.Attrs, true); ok {
		return unknownSchema(fmt.Sprintf("attribute %s on %s may change serde's view and is not modelled", a, d.Name))
	}
	for _, f := range d.Fields {
		if a, ok := unreadAttr(f.Attrs, false); ok {
			return unknownSchema(fmt.Sprintf("attribute %s on field %s of %s may change serde's view and is not modelled", a, f.Name, d.Name))
		}
	}
	for _, v := range d.Variants {
		if a, ok := unreadAttr(v.Attrs, false); ok {
			return unknownSchema(fmt.Sprintf("attribute %s on variant %s of %s may change serde's view and is not modelled", a, v.Name, d.Name))
		}
	}
	subst := map[string]*tref{}
	for i, g := range d.Generics {
		subst[g] = t.arg(i)
	}
	renameAll := ""
	if a, ok := serdeAttr(d.Serde, "rename_all"); ok {
		renameAll = a.Value
		if _, known := renameField("x", renameAll); !known {
			return unknownSchema("rename_all " + renameAll + " is not modelled")
		}
	}
	switch d.Kind {
	case "unit_struct":
		return routeshape.Schema{Type: routeshape.TypeNull}
	case "tuple_struct":
		if len(d.Fields) != 1 || len(d.Fields[0].Serde) > 0 {
			return unknownSchema(d.Name + " is a tuple struct (a fixed-length array)")
		}
		return s.schema(s.resolveType(d.Fields[0].Type, t.decl.file, d.Scope, subst, 0), c)
	case "enum":
		return s.enumSchema(d, renameAll, c)
	case "struct":
	default:
		return unknownSchema(d.Name + " is not read")
	}
	_, containerDefault := serdeAttr(d.Serde, "default")
	_, deny := serdeAttr(d.Serde, "deny_unknown_fields")
	obj := routeshape.Schema{Type: routeshape.TypeObject, Additional: routeshape.AdditionalAllowed, Fields: []routeshape.Field{}}
	if c.view == viewSer || deny {
		obj.Additional = routeshape.AdditionalForbidden
	}
	seen := map[string]bool{}
	for _, f := range d.Fields {
		field, keep := s.fieldSchema(f, t.decl.file, d.Scope, subst, renameAll, containerDefault, c)
		if !keep {
			continue
		}
		if field.Name == "" {
			// flatten: the object takes fields this reader does not list.
			obj.Additional = routeshape.AdditionalUnknown
			c.partial = true
			continue
		}
		if seen[field.Name] {
			return unknownSchema("two fields of " + d.Name + " share the name " + field.Name)
		}
		seen[field.Name] = true
		obj.Fields = append(obj.Fields, field)
	}
	return obj
}

// fieldSchema reads one struct field for the view. keep is false for a
// skipped field; an empty Name means a flattened field.
func (s *axumShaper) fieldSchema(f facts.RustField, file string, scope int, subst map[string]*tref, renameAll string, containerDefault bool, c *schemaCtx) (routeshape.Field, bool) {
	name := strings.TrimPrefix(f.Name, "r#")
	unknownField := func(reason string) (routeshape.Field, bool) {
		return routeshape.Field{Name: name, Presence: routeshape.PresenceUnknown, Schema: unknownSchema(reason)}, true
	}
	for _, a := range f.Serde {
		if a.Nested || !facts.Builtin(a.Name, fieldSafe) {
			if a.Name == "rename" && a.Nested {
				return unknownField("rename with separate serialize and deserialize names is not modelled")
			}
			return unknownField("serde attribute " + a.Name + " is not modelled")
		}
	}
	has := func(n string) bool { _, ok := serdeAttr(f.Serde, n); return ok }
	if has("skip") || c.view == viewSer && has("skip_serializing") || c.view != viewSer && has("skip_deserializing") {
		return routeshape.Field{}, false
	}
	if has("flatten") {
		return routeshape.Field{}, true
	}
	if a, ok := serdeAttr(f.Serde, "rename"); ok {
		name = a.Value
	} else if renameAll != "" {
		renamed, ok := renameField(name, renameAll)
		if !ok {
			return unknownField("rename_all " + renameAll + " cannot rename field " + name)
		}
		name = renamed
	}
	t := s.resolveType(f.Type, file, scope, subst, 0)
	out := routeshape.Field{Name: name, Presence: routeshape.PresenceUnknown}
	custom := has("with") || c.view == viewSer && has("serialize_with") || c.view != viewSer && has("deserialize_with")
	switch {
	case custom:
		out.Schema = unknownSchema("custom serde function for field " + f.Name)
	case c.view == viewForm && !s.formScalar(t, 0):
		// serde_urlencoded reads a value from one string: only scalars,
		// unit enums and newtypes of them (row SH-X4).
		out.Schema = unknownSchema("type " + t.text + " of field " + f.Name + " is not read from URL-encoded data")
	default:
		out.Schema = s.schema(t, c)
	}
	switch c.view {
	case viewSer:
		if has("skip_serializing_if") {
			out.Presence = routeshape.Optional
		}
	default:
		switch {
		case has("default") || containerDefault:
			out.Presence = routeshape.Optional
		case custom || has("alias"):
			// A custom deserializer decides about a missing field; an
			// alias accepts the field under another name.
			out.Presence = routeshape.PresenceUnknown
		case optionField(t):
			out.Presence = routeshape.Optional
		default:
			out.Presence = routeshape.Required
		}
	}
	return out, true
}

func (s *axumShaper) enumSchema(d *facts.RustTypeDecl, renameAll string, c *schemaCtx) routeshape.Schema {
	var values []any
	for _, v := range d.Variants {
		if v.Kind != "unit" {
			return unknownSchema(d.Name + " has variants with data")
		}
		for _, a := range v.Serde {
			if a.Nested || !facts.Builtin(a.Name, "rename skip skip_serializing skip_deserializing") {
				return unknownSchema("serde attribute " + a.Name + " on a variant of " + d.Name + " is not modelled")
			}
		}
		has := func(n string) bool { _, ok := serdeAttr(v.Serde, n); return ok }
		if has("skip") || c.view == viewSer && has("skip_serializing") || c.view != viewSer && has("skip_deserializing") {
			continue
		}
		name := strings.TrimPrefix(v.Name, "r#")
		if a, ok := serdeAttr(v.Serde, "rename"); ok {
			name = a.Value
		} else if renameAll != "" {
			renamed, ok := renameVariant(name, renameAll)
			if !ok {
				return unknownSchema("rename_all " + renameAll + " cannot rename variant " + name)
			}
			name = renamed
		}
		values = append(values, name)
	}
	if len(values) == 0 {
		return unknownSchema(d.Name + " has no variant serde reads")
	}
	return routeshape.Schema{Type: routeshape.TypeEnum, Enum: values}
}

// renameField applies serde's rename_all rule to a struct field
// (serde_derive 1.0.228 internals/case.rs, RenameRule::apply_to_field):
// lowercase and snake_case leave the field unchanged, PascalCase drops
// each `_` and upper-cases the next character, camelCase is PascalCase
// with the first character lowered. ok is false for an unknown rule, or
// where serde's own code would fail (a field that is all underscores, or
// starts with a character that is not ASCII, under camelCase).
func renameField(field, rule string) (string, bool) {
	switch rule {
	case "lowercase", "snake_case":
		return field, true
	case "UPPERCASE", "SCREAMING_SNAKE_CASE":
		return asciiUpper(field), true
	case "PascalCase":
		return pascalField(field), true
	case "camelCase":
		pascal := pascalField(field)
		if pascal == "" || pascal[0] >= 0x80 {
			return field, false
		}
		return asciiLower(pascal[:1]) + pascal[1:], true
	case "kebab-case":
		return strings.ReplaceAll(field, "_", "-"), true
	case "SCREAMING-KEBAB-CASE":
		return strings.ReplaceAll(asciiUpper(field), "_", "-"), true
	}
	return field, false
}

func pascalField(field string) string {
	var b strings.Builder
	capitalize := true
	for _, r := range field {
		switch {
		case r == '_':
			capitalize = true
		case capitalize:
			b.WriteString(asciiUpper(string(r)))
			capitalize = false
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// renameVariant applies serde's rename_all rule to an enum variant
// (RenameRule::apply_to_variant): snake_case puts `_` before every
// upper-case character but the first and lowers it (ASCII only).
func renameVariant(variant, rule string) (string, bool) {
	snake := func() string {
		var b strings.Builder
		for i, r := range variant {
			if i > 0 && unicode.IsUpper(r) {
				b.WriteByte('_')
			}
			b.WriteString(asciiLower(string(r)))
		}
		return b.String()
	}
	switch rule {
	case "PascalCase":
		return variant, true
	case "lowercase":
		return asciiLower(variant), true
	case "UPPERCASE":
		return asciiUpper(variant), true
	case "camelCase":
		if variant == "" || variant[0] >= 0x80 {
			return variant, false
		}
		return asciiLower(variant[:1]) + variant[1:], true
	case "snake_case":
		return snake(), true
	case "SCREAMING_SNAKE_CASE":
		return asciiUpper(snake()), true
	case "kebab-case":
		return strings.ReplaceAll(snake(), "_", "-"), true
	case "SCREAMING-KEBAB-CASE":
		return strings.ReplaceAll(asciiUpper(snake()), "_", "-"), true
	}
	return variant, false
}

// asciiUpper and asciiLower change ASCII letters only, as Rust's
// to_ascii_uppercase and to_ascii_lowercase.
func asciiUpper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 'a' + 'A'
		}
	}
	return string(b)
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c - 'A' + 'a'
		}
	}
	return string(b)
}

// inertAttrs are attributes that never change serde's derived code:
// lints, documentation, layout, and the helper attributes of derives that
// do not touch serde (validator, schemars, utoipa, sqlx, diesel, ts-rs,
// thiserror). `cfg` is inert on a type (the type exists or not) but not
// on a field or variant (which may then be missing).
const inertAttrs = "doc allow warn deny forbid expect deprecated must_use non_exhaustive repr default validate garde schemars schema sqlx diesel ts param into_params oai graphql builder error from source backtrace"

// unreadAttr returns the first attribute that may rewrite serde's view:
// an attribute macro (`serde_as`, `skip_serializing_none`, any unknown
// one), a `cfg_attr` applying serde, a derive or another such attribute,
// or (outside a type) `cfg`.
func unreadAttr(attrs []string, item bool) (string, bool) {
	for _, a := range attrs {
		if inner, ok := strings.CutPrefix(a, "cfg_attr("); ok {
			for _, applied := range strings.Split(strings.TrimSuffix(inner, ")"), ",") {
				if !facts.Builtin(applied, inertAttrs) {
					return a, true
				}
			}
			continue
		}
		if a == "cfg" && item || facts.Builtin(a, inertAttrs) {
			continue
		}
		return a, true
	}
	return "", false
}

// optionField reports a field serde reads as missing-means-None: an
// Option, also behind Box, Rc or Arc (their Deserialize forwards to the
// Option's, and a missing field deserializes an Option to None).
func optionField(t *tref) bool {
	for t.std("Box Rc Arc") && len(t.args) == 1 {
		t = t.args[0]
	}
	return t.std("Option")
}

// formScalar reports a type serde_urlencoded 0.7 reads from one value
// (its Part deserializer): bool, the integers up to 64 bits, floats,
// strings and char, a unit enum, a newtype of one of these, or an Option
// of one (not nested).
func (s *axumShaper) formScalar(t *tref, depth int) bool {
	if t == nil || depth > 4 {
		return false
	}
	switch {
	case t.std("bool i8 i16 i32 i64 isize u8 u16 u32 u64 usize f32 f64 String char"):
		return true
	case t.std("Option") && len(t.args) == 1 && depth == 0:
		return s.formScalar(t.args[0], depth+1)
	case t.kind == "local" && t.decl.decl.Kind == "enum":
		for _, v := range t.decl.decl.Variants {
			if v.Kind != "unit" {
				return false
			}
		}
		return true
	case t.kind == "local" && t.decl.decl.Kind == "tuple_struct" && len(t.decl.decl.Fields) == 1:
		d := t.decl.decl
		subst := map[string]*tref{}
		for i, g := range d.Generics {
			subst[g] = t.arg(i)
		}
		return s.formScalar(s.resolveType(d.Fields[0].Type, t.decl.file, d.Scope, subst, 0), depth+1)
	}
	return false
}

// indexTypes collects the cached type declarations and functions of
// every file.
func (s *axumShaper) index(files map[string]facts.Facts) {
	for _, file := range facts.Paths(files) {
		r := files[file].Rust
		if r == nil || r.Shape == nil {
			continue
		}
		for i := range r.Shape.Types {
			s.types[r.Shape.Types[i].Identity] = &axumTypeDecl{file: file, decl: &r.Shape.Types[i]}
		}
		for i := range r.Shape.Fns {
			f := &r.Shape.Fns[i]
			s.fns[f.Identity] = &axumFn{file: file, fn: f}
		}
	}
	ids := make([]string, 0, len(s.fns))
	for id := range s.fns {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		f := s.fns[id]
		s.steps[axumStepKey(s.step(id))] = id
		if f.fn.Trait != "" {
			s.traitFns = append(s.traitFns, id)
		}
	}
}

// step names a function for chains: its file, line and qualified name.
func (s *axumShaper) step(id string) routeshape.Step {
	f := s.fns[id]
	_, _, name, _ := contract.ParseIdentity(id)
	if i := strings.Index(name, "@"); i >= 0 {
		name = name[:i]
	}
	return routeshape.Step{File: f.file, Line: f.fn.Line, Function: name}
}

func axumStepKey(st routeshape.Step) string {
	return fmt.Sprintf("%s\x00%d\x00%s", st.File, st.Line, st.Function)
}

package routes

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// Schemas of Go types for the route shape (rows SH-G26..SH-G31 of
// docs/contract/claims/route-shape-go.md). A type is read from the cached
// struct and named-type declarations of the module's own packages; the
// field set follows encoding/json's rules (the json tag's name, "-",
// omitempty and string options, unexported fields skipped, embedded
// structs flattened with Go's dominance rules) or gin's form mapping
// (form and uri tags, the field name by default, binding:"required").
// Every type Graphvein cannot read the way encoding/json or gin would is
// an unknown schema with the reason, never a guess.

// goSchemaMode is how a value is read or written.
type goSchemaMode int

const (
	// goJSONRequest: encoding/json decodes the body into the type (gin's
	// JSON binder, json.Decoder). gin marks whether binding tags apply.
	goJSONRequest goSchemaMode = iota
	// goJSONResponse: encoding/json encodes the value.
	goJSONResponse
)

// goTypeRef is a type expression and the file whose names it uses.
type goTypeRef struct {
	File string       `json:"f"`
	Type facts.GoExpr `json:"t"`
	// Shadow are the type names declared in the function the expression
	// was written in or a function enclosing it (GoShapeFunc.LocalTypes):
	// such a name is not the package's type of that name.
	Shadow []string `json:"s,omitempty"`
}

// goShadow is the set of a function's local type names.
func goShadow(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	out := map[string]bool{}
	for _, n := range names {
		out[n] = true
	}
	return out
}

// goSchemaBuilder builds schemas for one route.
type goSchemaBuilder struct {
	l *goLinker
	// binding applies gin's binding:"required" (a gin binder validates).
	binding bool
	// gin marks gin's JSON binder, which a program may set to refuse
	// unknown fields (binding.EnableDecoderDisallowUnknownFields).
	gin bool
	// dive reports that the elements of the next slice, array or map
	// type are validated: gin's validator validates the elements of the
	// bound value itself, and those of a field only under a dive tag.
	dive bool
	mode goSchemaMode
	// stack holds the named types being expanded (recursion).
	stack map[string]bool
	// shadow are the local type names of the function the root type
	// expression was written in (T029 W2 review; not read, row D):
	// a bare name among them is a function-local type, which the shape
	// reader does not resolve. Cleared when the reader enters a module
	// declaration, whose names are resolved at package scope.
	shadow map[string]bool
}

// goNamedDecl is a resolved module named type.
type goNamedDecl struct {
	file  string
	strct *facts.GoStruct
	named *facts.GoNamedType
	key   string
	name  string
}

// lookupType resolves a named type expression (T or pkg.T) to its module
// declaration. ok is false with a reason when it is not one.
func (b *goSchemaBuilder) lookupType(file string, t facts.GoExpr) (goNamedDecl, string) {
	var files []string
	name := ""
	switch t.Kind {
	case facts.GoIdentifier:
		if b.shadow[t.Name] {
			return goNamedDecl{}, fmt.Sprintf("type %s is declared in the function, not read", t.Name)
		}
		files, name = b.l.ownFiles(file), t.Name
	case facts.GoSelector:
		if t.Object == nil || t.Object.Kind != facts.GoIdentifier {
			return goNamedDecl{}, "type expression not read"
		}
		imp, ok := b.l.moduleImports(file)[t.Object.Name]
		if !ok {
			return goNamedDecl{}, fmt.Sprintf("type %s.%s is outside the repository", t.Object.Name, t.Name)
		}
		files, name = imp.files, t.Name
	default:
		return goNamedDecl{}, "type expression not read"
	}
	var found []goNamedDecl
	for _, p := range files {
		g := b.l.files[p].Go
		if g == nil {
			continue
		}
		for i := range g.Structs {
			if g.Structs[i].Name == name {
				found = append(found, goNamedDecl{file: p, strct: &g.Structs[i], key: pathDir(p) + "." + name, name: name})
			}
		}
		for i := range g.Types {
			if g.Types[i].Name == name {
				found = append(found, goNamedDecl{file: p, named: &g.Types[i], key: pathDir(p) + "." + name, name: name})
			}
		}
	}
	switch len(found) {
	case 0:
		return goNamedDecl{}, fmt.Sprintf("type %s is not declared in the module", name)
	case 1:
		return found[0], ""
	}
	return goNamedDecl{}, fmt.Sprintf("type %s is declared more than once (build constraints)", name)
}

func pathDir(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return "."
}

// marshaler reports the encoding/json method a module type declares.
func (b *goSchemaBuilder) marshaler(d goNamedDecl) string {
	for _, p := range b.l.ownFiles(d.file) {
		g := b.l.files[p].Go
		if g == nil {
			continue
		}
		for _, m := range g.Marshalers {
			if m.Receiver == d.name {
				return m.Name
			}
		}
	}
	return ""
}

func unknownSchema(reason string) routeshape.Schema {
	return routeshape.Schema{Type: routeshape.TypeUnknown, Reason: reason}
}

// goBasic maps the predeclared types encoding/json encodes as JSON
// scalars.
var goBasic = map[string]routeshape.SchemaType{
	"string": routeshape.TypeString, "bool": routeshape.TypeBoolean,
	"int": routeshape.TypeInteger, "int8": routeshape.TypeInteger, "int16": routeshape.TypeInteger, "int32": routeshape.TypeInteger, "int64": routeshape.TypeInteger,
	"uint": routeshape.TypeInteger, "uint8": routeshape.TypeInteger, "uint16": routeshape.TypeInteger, "uint32": routeshape.TypeInteger, "uint64": routeshape.TypeInteger, "uintptr": routeshape.TypeInteger,
	"byte": routeshape.TypeInteger, "rune": routeshape.TypeInteger,
	"float32": routeshape.TypeNumber, "float64": routeshape.TypeNumber,
}

// goStdTypes are the standard-library types whose JSON form is fixed.
var goStdTypes = map[string]routeshape.SchemaType{
	"time.Time":     routeshape.TypeString, // RFC 3339 (MarshalJSON)
	"time.Duration": routeshape.TypeInteger,
}

// schema returns the schema of type t (in file's names) at nesting depth.
func (b *goSchemaBuilder) schema(file string, t facts.GoExpr, depth int) routeshape.Schema {
	if depth >= routeshape.MaxSchemaDepth-2 {
		return unknownSchema("nested too deeply")
	}
	switch t.Kind {
	case facts.GoIdentifier:
		if st, ok := goBasic[t.Name]; ok {
			return routeshape.Schema{Type: st}
		}
		switch t.Name {
		case "any":
			return unknownSchema("interface type: the value's dynamic type is not read")
		case "error":
			return unknownSchema("error type")
		case "complex64", "complex128":
			return unknownSchema("encoding/json cannot encode complex numbers")
		}
		return b.named(file, t, depth)
	case facts.GoSelector:
		if t.Object != nil && t.Object.Kind == facts.GoIdentifier {
			if path := goImportPath(b.l, file, t.Object.Name); path != "" {
				short := path
				if i := strings.LastIndexByte(short, '/'); i >= 0 {
					short = short[i+1:]
				}
				if st, ok := goStdTypes[short+"."+t.Name]; ok && (path == "time") {
					return routeshape.Schema{Type: st}
				}
			}
		}
		return b.named(file, t, depth)
	case facts.GoPointer:
		if t.Object == nil {
			return unknownSchema("type expression not read")
		}
		s := b.schema(file, *t.Object, depth+1)
		if s.Type != routeshape.TypeUnknown {
			s.Nullable = true
		}
		return s
	case facts.GoSlice, facts.GoArray:
		if t.Object == nil {
			return unknownSchema("type expression not read")
		}
		if t.Kind == facts.GoSlice && t.Object.Kind == facts.GoIdentifier && (t.Object.Name == "byte" || t.Object.Name == "uint8") {
			// encoding/json writes a []byte as a base64 string.
			s := routeshape.Schema{Type: routeshape.TypeString}
			if b.mode == goJSONResponse {
				s.Nullable = true
			}
			return s
		}
		saved := b.binding
		b.binding = b.binding && b.dive
		b.dive = false
		items := b.schema(file, *t.Object, depth+1)
		b.binding = saved
		s := routeshape.Schema{Type: routeshape.TypeArray, Items: &items}
		if t.Kind == facts.GoSlice && b.mode == goJSONResponse {
			s.Nullable = true // a nil slice is null
		}
		return s
	case facts.GoMap:
		if t.Left == nil || t.Right == nil || !b.mapKey(file, *t.Left) {
			return unknownSchema("map key type not read as a string or integer")
		}
		s := routeshape.Schema{Type: routeshape.TypeObject, Additional: routeshape.AdditionalAllowed}
		if b.mode == goJSONResponse {
			s.Nullable = true
		}
		return s
	case facts.GoInterface:
		return unknownSchema("interface type: the value's dynamic type is not read")
	case facts.GoChan, facts.GoFuncType:
		return unknownSchema("encoding/json cannot encode channels or functions")
	case facts.GoStructType:
		return unknownSchema("anonymous struct type not read")
	case facts.GoIndex:
		return unknownSchema("generic type instance not read")
	}
	return unknownSchema("type expression not read")
}

// mapKey reports a map key type encoding/json writes as an object key:
// string or integer kinds (TextMarshaler keys are not read).
func (b *goSchemaBuilder) mapKey(file string, t facts.GoExpr) bool {
	if t.Kind != facts.GoIdentifier {
		return false
	}
	st, ok := goBasic[t.Name]
	return ok && (st == routeshape.TypeString || st == routeshape.TypeInteger)
}

// named expands a module named type.
func (b *goSchemaBuilder) named(file string, t facts.GoExpr, depth int) routeshape.Schema {
	d, why := b.lookupType(file, t)
	if why != "" {
		return unknownSchema(why)
	}
	// The resolved declaration is used whatever its schema reads as (row
	// MT-6): a change to it changes what the route sends or accepts.
	shadow := b.shadow
	b.shadow = nil
	s := b.namedSchema(d, depth)
	b.shadow = shadow
	if id := b.declIdentity(d); id != "" && !containsString(s.Decls, id) {
		s.Decls = append(s.Decls, id)
	}
	return s
}

// declIdentity is the node identity of a resolved module type: the one
// declaration node of its file with its name (row MT-6), or empty.
func (b *goSchemaBuilder) declIdentity(d goNamedDecl) string {
	found := ""
	for _, n := range b.l.files[d.file].Nodes {
		if n.QualifiedName != d.name || !routeshape.DeclarationKind(n.Kind) {
			continue
		}
		if found != "" {
			return ""
		}
		found = n.Identity
	}
	return found
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// namedSchema is named's schema of a resolved declaration.
func (b *goSchemaBuilder) namedSchema(d goNamedDecl, depth int) routeshape.Schema {
	if m := b.marshaler(d); m != "" {
		return unknownSchema(fmt.Sprintf("type %s has a %s method", d.name, m))
	}
	if b.stack[d.key] {
		return unknownSchema(fmt.Sprintf("recursive type %s", d.name))
	}
	b.stack[d.key] = true
	defer delete(b.stack, d.key)
	if d.named != nil {
		if len(d.named.TypeParams) > 0 {
			return unknownSchema(fmt.Sprintf("generic type %s", d.name))
		}
		switch d.named.Type.Kind {
		case facts.GoSelector, facts.GoIndex:
			// A defined type does not keep the methods of its underlying
			// type (type Stamp time.Time encodes as {}).
			return unknownSchema(fmt.Sprintf("defined type %s of another named type", d.name))
		case facts.GoIdentifier:
			if _, basic := goBasic[d.named.Type.Name]; !basic {
				return unknownSchema(fmt.Sprintf("defined type %s of another named type", d.name))
			}
		}
		return b.schema(d.file, d.named.Type, depth+1)
	}
	if len(d.strct.TypeParams) > 0 {
		return unknownSchema(fmt.Sprintf("generic type %s", d.name))
	}
	return b.object(d, depth)
}

// goJSONField is one candidate field of encoding/json's typeFields.
type goJSONField struct {
	name     string
	tagged   bool
	depth    int
	order    []int
	file     string
	typ      facts.GoExpr
	tag      string
	optional bool
	quoted   bool
	// viaPtr: promoted through an embedded pointer.
	viaPtr bool
}

// object reads a struct's fields as encoding/json's typeFields does:
// breadth first through embedded structs, then for each JSON name the
// dominant field (the shallowest; at equal depth the only tagged one;
// otherwise none). An embedded type that is not a module struct cuts the
// field set at its depth: fields at its depth or deeper may be shadowed by
// fields Graphvein cannot see, so they are left out and additional
// properties are unknown.
func (b *goSchemaBuilder) object(root goNamedDecl, depth int) routeshape.Schema {
	type level struct {
		d     goNamedDecl
		order []int
		// ptr: reached through an embedded pointer (never validated when
		// nil, so its fields are never required).
		ptr bool
	}
	current := []level{{d: root}}
	count := map[string]int{root.key: 1}
	// embedded are the embedded structs whose fields were read: the
	// object uses their declarations too (row MT-6).
	var embedded []goNamedDecl
	var fields []goJSONField
	visited := map[string]bool{}
	cut := -1 // depth from which fields are unknown
	setCut := func(d int) {
		if cut < 0 || d < cut {
			cut = d
		}
	}
	for lvl := 0; len(current) > 0; lvl++ {
		if cut >= 0 && lvl >= cut {
			break
		}
		nextCount := map[string]int{}
		var next []level
		for _, c := range current {
			if visited[c.d.key] {
				continue
			}
			visited[c.d.key] = true
			add := func(f goJSONField) {
				f.viaPtr = c.ptr
				fields = append(fields, f)
				if count[c.d.key] > 1 {
					// Several embeddings of one type at a level: the
					// duplicate makes dominance drop the name.
					fields = append(fields, f)
				}
			}
			for fi, f := range c.d.strct.Fields {
				order := append(append([]int(nil), c.order...), fi)
				if f.Embedded {
					et := f.Type
					if et.Kind == facts.GoPointer && et.Object != nil {
						et = *et.Object
					}
					ename := goTypeName(et)
					tag := reflect.StructTag(f.Tag).Get("json")
					if tag == "-" {
						continue
					}
					name, _ := goTagName(tag)
					if name != "" {
						// A tagged embedded field is a named field.
						add(b.jsonField(c.d.file, name, true, lvl, order, f.Type, f.Tag))
						continue
					}
					ed, why := b.lookupType(c.d.file, et)
					if why != "" {
						// The embedded type's method set is not visible: a
						// promoted MarshalJSON (time.Time, a library type)
						// may write the whole value.
						return unknownSchema(fmt.Sprintf("embedded type %s: %s", ename, why))
					}
					if ed.strct != nil && len(ed.strct.TypeParams) > 0 {
						return unknownSchema(fmt.Sprintf("embedded generic type %s", ename))
					}
					if ed.named != nil {
						switch ed.named.Type.Kind {
						case facts.GoIdentifier, facts.GoSelector, facts.GoIndex, facts.GoStructType:
							// May be a struct type: not read.
							setCut(lvl + 1)
						default:
							// An embedded non-struct type is a field named
							// by its type when exported.
							if goExported(ename) {
								add(b.jsonField(c.d.file, ename, false, lvl, order, f.Type, f.Tag))
							}
						}
						continue
					}
					if m := b.marshaler(ed); m != "" {
						// The method is promoted: encoding/json calls it
						// for the whole outer value.
						return unknownSchema(fmt.Sprintf("embedded type %s has a %s method", ed.name, m))
					}
					if f.Pointer && !goExported(ename) {
						// encoding/json cannot set an embedded pointer to
						// an unexported struct type.
						return unknownSchema(fmt.Sprintf("embedded pointer to unexported type %s", ename))
					}
					nextCount[ed.key]++
					if nextCount[ed.key] == 1 {
						next = append(next, level{d: ed, order: order, ptr: c.ptr || f.Pointer})
						embedded = append(embedded, ed)
					}
					continue
				}
				for ni, n := range f.Names {
					if !goExported(n) {
						continue
					}
					tag := reflect.StructTag(f.Tag).Get("json")
					if tag == "-" {
						continue
					}
					name, _ := goTagName(tag)
					tagged := name != ""
					if !tagged {
						name = n
					}
					add(b.jsonField(c.d.file, name, tagged, lvl, append(append([]int(nil), order...), ni), f.Type, f.Tag))
				}
			}
		}
		current, count = next, nextCount
	}
	// Dominance: per name, the shallowest fields; one tagged wins a tie.
	byName := map[string][]goJSONField{}
	var names []string
	for _, f := range fields {
		if cut >= 0 && f.depth >= cut {
			continue
		}
		if _, ok := byName[f.name]; !ok {
			names = append(names, f.name)
		}
		byName[f.name] = append(byName[f.name], f)
	}
	var kept []goJSONField
	for _, n := range names {
		fs := byName[n]
		min := fs[0].depth
		for _, f := range fs {
			if f.depth < min {
				min = f.depth
			}
		}
		var top []goJSONField
		for _, f := range fs {
			if f.depth == min {
				top = append(top, f)
			}
		}
		if len(top) > 1 {
			var tagged []goJSONField
			for _, f := range top {
				if f.tagged {
					tagged = append(tagged, f)
				}
			}
			if len(tagged) != 1 {
				continue
			}
			top = tagged
		}
		kept = append(kept, top[0])
	}
	sort.SliceStable(kept, func(i, j int) bool { return lessOrder(kept[i].order, kept[j].order) })
	out := routeshape.Schema{Type: routeshape.TypeObject, Additional: routeshape.AdditionalAllowed, Fields: []routeshape.Field{}}
	switch {
	case b.mode == goJSONResponse:
		// encoding/json writes exactly the fields.
		out.Additional = routeshape.AdditionalForbidden
	case b.gin:
		out.Additional = routeshape.AdditionalUnknown
	}
	if cut >= 0 {
		out.Additional = routeshape.AdditionalUnknown
	}
	for _, f := range kept {
		out.Fields = append(out.Fields, b.field(f, depth))
	}
	for _, ed := range embedded {
		if id := b.declIdentity(ed); id != "" && !containsString(out.Decls, id) {
			out.Decls = append(out.Decls, id)
		}
	}
	return out
}

func lessOrder(a, b []int) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

// jsonField reads one field's options.
func (b *goSchemaBuilder) jsonField(file, name string, tagged bool, depth int, order []int, t facts.GoExpr, tag string) goJSONField {
	_, opts := goTagName(reflect.StructTag(tag).Get("json"))
	f := goJSONField{name: name, tagged: tagged, depth: depth, order: order, file: file, typ: t, tag: tag}
	for _, o := range opts {
		switch o {
		case "omitempty", "omitzero":
			f.optional = true
		case "string":
			f.quoted = true
		}
	}
	return f
}

// field builds one object field: its schema, presence and nullability.
func (b *goSchemaBuilder) field(f goJSONField, depth int) routeshape.Field {
	b.dive = goBindingDive(f.tag)
	s := b.schema(f.file, f.typ, depth+1)
	b.dive = false
	if f.quoted && s.Type != routeshape.TypeUnknown && s.Type != routeshape.TypeObject && s.Type != routeshape.TypeArray {
		// The string option writes a scalar as a JSON string.
		s = routeshape.Schema{Type: routeshape.TypeString, Nullable: s.Nullable}
	}
	presence := routeshape.PresenceUnknown
	switch b.mode {
	case goJSONResponse:
		// A field promoted through a nil embedded pointer is not written.
		if f.optional || f.viaPtr {
			presence = routeshape.Optional
		}
	case goJSONRequest:
		if b.binding && !f.viaPtr && goBindingRequired(f.tag) && !b.structKind(f.file, f.typ) {
			presence = routeshape.Required
		} else if s.Type != routeshape.TypeUnknown {
			// encoding/json accepts null for every field (a no-op).
			s.Nullable = true
		}
	}
	return routeshape.Field{Name: f.name, Presence: presence, Schema: s}
}

// goBindingDive reports a dive in a binding tag: the validator then
// validates the elements of the field's slice, array or map.
func goBindingDive(tag string) bool {
	v, _ := reflect.StructTag(tag).Lookup("binding")
	for _, part := range strings.Split(v, ",") {
		if strings.TrimSpace(part) == "dive" {
			return true
		}
	}
	return false
}

// structKind reports a non-pointer field type whose kind is (or may be)
// a struct other than time.Time: go-playground validator's required does
// not check such a field (gin does not enable WithRequiredStructEnabled).
func (b *goSchemaBuilder) structKind(file string, t facts.GoExpr) bool {
	switch t.Kind {
	case facts.GoIdentifier:
		if _, basic := goBasic[t.Name]; basic {
			return false
		}
		d, why := b.lookupType(file, t)
		if why != "" {
			return true
		}
		if d.strct != nil {
			return true
		}
		return d.named != nil && d.named.Type.Kind != facts.GoSlice && d.named.Type.Kind != facts.GoMap && d.named.Type.Kind != facts.GoArray && !(d.named.Type.Kind == facts.GoIdentifier && goBasic[d.named.Type.Name] != "")
	case facts.GoSelector:
		return !(t.Name == "Time" && t.Object != nil && t.Object.Kind == facts.GoIdentifier && goImportPath(b.l, file, t.Object.Name) == "time")
	case facts.GoStructType, facts.GoIndex:
		return true
	}
	return false
}

// goBindingRequired reports gin's binding:"required" (go-playground
// validator's required, without omitempty or a dive before it).
func goBindingRequired(tag string) bool {
	v, ok := reflect.StructTag(tag).Lookup("binding")
	if !ok {
		return false
	}
	for _, part := range strings.Split(v, ",") {
		switch strings.TrimSpace(part) {
		case "required":
			return true
		case "omitempty", "dive", "-":
			return false
		}
	}
	return false
}

// goTagName splits a json tag into its name (empty when not a valid name)
// and options, as encoding/json does.
func goTagName(tag string) (string, []string) {
	parts := strings.Split(tag, ",")
	name := parts[0]
	if !goValidTag(name) {
		name = ""
	}
	return name, parts[1:]
}

// goValidTag is encoding/json's isValidTag.
func goValidTag(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", c):
		case !unicode.IsLetter(c) && !unicode.IsDigit(c):
			return false
		}
	}
	return true
}

func goExported(name string) bool {
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
}

// goTypeName is the name of T or pkg.T.
func goTypeName(t facts.GoExpr) string {
	switch t.Kind {
	case facts.GoIdentifier, facts.GoSelector:
		return t.Name
	case facts.GoIndex:
		if t.Object != nil {
			return goTypeName(*t.Object)
		}
	}
	return ""
}

// goFormParam is one parameter gin's form mapping reads from a struct.
type goFormParam struct {
	name     string
	presence routeshape.Presence
	schema   *routeshape.Schema
}

// formParams reads a struct as gin's form, query and uri binders map it
// (binding/form_mapping.go): exported fields by the tag's name or the
// field name, "-" skipped, embedded structs flattened, scalars, slices of
// scalars and time.Time; binding:"required" makes a field required and a
// default option optional. Any other field type makes the whole struct
// unread (ok false), since gin's mapping of it is not modelled.
func (b *goSchemaBuilder) formParams(file string, t facts.GoExpr, tagName string, depth int) ([]goFormParam, string) {
	if t.Kind == facts.GoPointer && t.Object != nil {
		t = *t.Object
	}
	d, why := b.lookupType(file, t)
	if why != "" {
		return nil, why
	}
	if d.strct == nil {
		return nil, fmt.Sprintf("type %s is not a struct", d.name)
	}
	if len(d.strct.TypeParams) > 0 {
		return nil, fmt.Sprintf("generic type %s", d.name)
	}
	if depth > 8 || b.stack[d.key] {
		return nil, fmt.Sprintf("recursive type %s", d.name)
	}
	b.stack[d.key] = true
	defer delete(b.stack, d.key)
	shadow := b.shadow
	b.shadow = nil
	defer func() { b.shadow = shadow }()
	var out []goFormParam
	for _, f := range d.strct.Fields {
		tag := reflect.StructTag(f.Tag).Get(tagName)
		if tag == "-" {
			continue
		}
		name, opts := tag, ""
		if i := strings.IndexByte(tag, ','); i >= 0 {
			name, opts = tag[:i], tag[i+1:]
		}
		if f.Embedded {
			if name == "" {
				inner, why := b.formParams(d.file, f.Type, tagName, depth+1)
				if why != "" {
					return nil, why
				}
				out = append(out, inner...)
				continue
			}
			return nil, "a tagged embedded field is not read"
		}
		for _, n := range f.Names {
			if !goExported(n) {
				continue
			}
			pn := name
			if pn == "" {
				pn = n
			}
			s, ok := b.formSchema(d.file, f.Type, f.Tag)
			if !ok {
				return nil, fmt.Sprintf("field %s.%s has a type gin's form mapping reads in ways not modelled", d.name, n)
			}
			presence := routeshape.PresenceUnknown
			switch {
			case strings.Contains(opts, "default="):
				presence = routeshape.Optional
			case goBindingRequired(f.Tag):
				presence = routeshape.Required
			}
			out = append(out, goFormParam{name: pn, presence: presence, schema: &s})
		}
	}
	// Every parameter read from the struct uses its declaration as a
	// parameter model (row MT-6).
	if id := b.declIdentity(d); id != "" {
		for _, p := range out {
			if !containsString(p.schema.Decls, id) {
				p.schema.Decls = append(p.schema.Decls, id)
			}
		}
	}
	return out, ""
}

// formSchema is the schema of a form-mapped field type.
func (b *goSchemaBuilder) formSchema(file string, t facts.GoExpr, tag string) (routeshape.Schema, bool) {
	switch t.Kind {
	case facts.GoIdentifier:
		if st, ok := goBasic[t.Name]; ok {
			return routeshape.Schema{Type: st}, true
		}
		d, why := b.lookupType(file, t)
		if why == "" && d.named != nil && len(d.named.TypeParams) == 0 && d.named.Type.Kind == facts.GoIdentifier {
			if b.marshaler(d) != "" {
				return routeshape.Schema{}, false
			}
			if st, ok := goBasic[d.named.Type.Name]; ok {
				return routeshape.Schema{Type: st}, true
			}
		}
	case facts.GoSelector:
		if t.Object != nil && t.Object.Kind == facts.GoIdentifier && t.Name == "Time" && goImportPath(b.l, file, t.Object.Name) == "time" {
			switch reflect.StructTag(tag).Get("time_format") {
			case "unix", "unixmilli", "unixmicro", "unixnano":
				// gin reads the time as an integer count.
				return routeshape.Schema{Type: routeshape.TypeInteger}, true
			}
			return routeshape.Schema{Type: routeshape.TypeString}, true
		}
	case facts.GoPointer:
		if t.Object != nil {
			return b.formSchema(file, *t.Object, tag)
		}
	case facts.GoSlice, facts.GoArray:
		if t.Object != nil {
			if s, ok := b.formSchema(file, *t.Object, tag); ok && s.Type != routeshape.TypeArray {
				return routeshape.Schema{Type: routeshape.TypeArray, Items: &s}, true
			}
		}
	}
	return routeshape.Schema{}, false
}

// goImportPath is the import path file binds to name ("" when none, or
// when the name is shadowed by nothing the payload can see).
func goImportPath(l *goLinker, file, name string) string {
	for _, imp := range l.files[file].Imports {
		alias := ""
		for a := range imp.Bindings {
			alias = a
		}
		if !imp.ExplicitAlias {
			alias = goDefaultImportName(imp.Module)
		}
		if alias == name {
			return imp.Module
		}
	}
	return ""
}

// goDefaultImportName is the package name an unaliased import binds, for
// the packages the shape rules read; the last path element otherwise
// (versioned paths read their element before the version).
func goDefaultImportName(module string) string {
	parts := strings.Split(module, "/")
	last := parts[len(parts)-1]
	if len(parts) > 1 && len(last) > 1 && last[0] == 'v' && strings.Trim(last[1:], "0123456789") == "" {
		last = parts[len(parts)-2]
	}
	return last
}

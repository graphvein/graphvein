package routes

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/python"
)

// This file resolves Python annotations to route shape schemas (rows
// SH-F4, SH-F8, SH-F21, SH-F22): builtins and a closed list of standard
// and Pydantic types, typing generics, enums and repository Pydantic or
// SQLModel models. Anything else is unknown with a reason, never a guess.

// Type categories, as FastAPI's parameter analysis sees them.
const (
	catScalar   = "scalar"   // a query or path parameter without a marker
	catSequence = "sequence" // a body field without a marker (H1)
	catMapping  = "mapping"
	catModel    = "model"
	catFile     = "file"
	catNone     = "none"
	catSpecial  = "special" // Request, Response, ... (row SH-F14)
	catUnknown  = "unknown"
)

// typeInfo is a resolved annotation.
type typeInfo struct {
	schema  routeshape.Schema
	cat     string
	special string // the framework value's class name for catSpecial
	enum    bool   // an Enum subclass or Literal (row SH-F8)
	alias   bool   // resolved through an Annotated alias (row SH-F7)
	// loose is true when every string a client sends validates (str,
	// bytes, Any, SecretStr, Path; lists of them): such a parameter alone
	// cannot make FastAPI answer 422 (row SH-F30).
	loose bool
	// stream is true for an iterable return annotation (row SH-F39).
	stream bool
}

func unknownType(format string, args ...any) typeInfo {
	return typeInfo{schema: routeshape.Schema{Type: routeshape.TypeUnknown, Reason: fmt.Sprintf(format, args...)}, cat: catUnknown}
}

// builtinTypes are the builtins an unbound name may be.
var builtinTypes = map[string]typeInfo{
	"int":       {schema: routeshape.Schema{Type: routeshape.TypeInteger}, cat: catScalar},
	"str":       {schema: routeshape.Schema{Type: routeshape.TypeString}, cat: catScalar, loose: true},
	"float":     {schema: routeshape.Schema{Type: routeshape.TypeNumber}, cat: catScalar},
	"bool":      {schema: routeshape.Schema{Type: routeshape.TypeBoolean}, cat: catScalar},
	"bytes":     {schema: routeshape.Schema{Type: routeshape.TypeString}, cat: catScalar, loose: true},
	"dict":      {schema: routeshape.Schema{Type: routeshape.TypeObject, Additional: routeshape.AdditionalAllowed}, cat: catMapping},
	"list":      {schema: routeshape.Schema{Type: routeshape.TypeArray, Items: &routeshape.Schema{Type: routeshape.TypeUnknown, Reason: "items not typed"}}, cat: catSequence},
	"set":       {schema: routeshape.Schema{Type: routeshape.TypeArray, Items: &routeshape.Schema{Type: routeshape.TypeUnknown, Reason: "items not typed"}}, cat: catSequence},
	"frozenset": {schema: routeshape.Schema{Type: routeshape.TypeArray, Items: &routeshape.Schema{Type: routeshape.TypeUnknown, Reason: "items not typed"}}, cat: catSequence},
}

// externalStrings are library types whose JSON form is a string
// (Pydantic v2 schemas).
var externalStrings = map[string]bool{
	"uuid.UUID": true, "datetime.datetime": true, "datetime.date": true, "datetime.time": true,
	"pydantic.EmailStr": true, "pydantic.networks.EmailStr": true, "pydantic.AnyUrl": true, "pydantic.HttpUrl": true,
	"pydantic.AnyHttpUrl": true, "pydantic.networks.AnyUrl": true, "pydantic.networks.HttpUrl": true,
	"pydantic.SecretStr": true, "pydantic.types.SecretStr": true, "pathlib.Path": true,
}

// looseStrings are the externalStrings every string validates as.
var looseStrings = map[string]bool{"pydantic.SecretStr": true, "pydantic.types.SecretStr": true, "pathlib.Path": true}

// streamTypes are the return annotations FastAPI streams item by item
// (row SH-F39).
var streamTypes = map[string]bool{
	"typing.Iterable": true, "typing.Iterator": true, "typing.Generator": true, "typing.AsyncIterable": true,
	"typing.AsyncIterator": true, "typing.AsyncGenerator": true, "collections.abc.Iterable": true,
	"collections.abc.Iterator": true, "collections.abc.Generator": true, "collections.abc.AsyncIterable": true,
	"collections.abc.AsyncIterator": true, "collections.abc.AsyncGenerator": true,
}

// allowedMetadata are the Annotated metadata (outside FastAPI's markers)
// that keep the annotated type's schema: documentation, and constraints
// and after-validators that run on the already validated value. Any other
// item (BeforeValidator, WrapValidator, PlainValidator, a repository
// value, ...) can change what is accepted (row SH-F22, D).
func allowedMetadata(name string) bool {
	switch name {
	case "pydantic.AfterValidator", "pydantic.functional_validators.AfterValidator",
		"pydantic.StringConstraints", "pydantic.types.StringConstraints",
		"typing_extensions.Doc", "annotated_doc.Doc":
		return true
	}
	return strings.HasPrefix(name, "annotated_types.")
}

// constraintMetadata reports a metadata call that rejects some values of
// its type (a 422 can be reached): an annotated_types constraint, or
// StringConstraints with a length or pattern.
func constraintMetadata(name string, call facts.RouteExpr) bool {
	switch {
	case strings.HasPrefix(name, "annotated_types."):
		return true
	case name == "pydantic.StringConstraints" || name == "pydantic.types.StringConstraints":
		for _, k := range call.Keywords {
			switch k.Name {
			case "min_length", "max_length", "pattern":
				return true
			}
		}
	}
	return false
}

// externalSpecial are the framework values FastAPI provides instead of
// reading a parameter (row SH-F14).
var externalSpecial = map[string]string{
	"fastapi.Request": "Request", "starlette.requests.Request": "Request", "fastapi.requests.Request": "Request",
	"fastapi.WebSocket": "WebSocket", "starlette.websockets.WebSocket": "WebSocket", "fastapi.websockets.WebSocket": "WebSocket",
	"starlette.requests.HTTPConnection": "HTTPConnection", "fastapi.requests.HTTPConnection": "HTTPConnection",
	"fastapi.Response": "Response", "starlette.responses.Response": "Response", "fastapi.responses.Response": "Response",
	"fastapi.BackgroundTasks": "BackgroundTasks", "starlette.background.BackgroundTasks": "BackgroundTasks", "fastapi.background.BackgroundTasks": "BackgroundTasks",
	"fastapi.security.SecurityScopes": "SecurityScopes", "fastapi.security.oauth2.SecurityScopes": "SecurityScopes",
}

// externalFiles are the upload types (row SH-F20).
var externalFiles = map[string]bool{"fastapi.UploadFile": true, "starlette.datastructures.UploadFile": true, "fastapi.datastructures.UploadFile": true}

// responseClasses are Response subclasses whose return annotation is not a
// response model (row SH-F27), with their media type ("" when FastAPI's
// spec gives none we read).
var responseClasses = map[string]string{
	"JSONResponse": "application/json", "ORJSONResponse": "application/json", "UJSONResponse": "application/json",
	"HTMLResponse": "text/html", "PlainTextResponse": "text/plain", "RedirectResponse": "", "StreamingResponse": "",
	"FileResponse": "", "Response": "", "EventSourceResponse": "",
}

// responseClassStatus is the status a response class sends when FastAPI
// passes none (no status_code=, row SH-F24): the class's own default.
func responseClassStatus(b python.Binding) (string, bool) {
	if _, ok := responseClass(b); !ok {
		return "", false
	}
	if strings.HasSuffix(fullName(b), ".RedirectResponse") {
		return "307", true
	}
	return "200", true
}

// responseClass reports whether a resolved binding is a Response class
// from fastapi.responses, starlette.responses or sse_starlette.
func responseClass(b python.Binding) (string, bool) {
	if !b.External {
		return "", false
	}
	name := fullName(b)
	at := strings.LastIndex(name, ".")
	if at < 0 {
		return "", false
	}
	module, short := name[:at], name[at+1:]
	switch module {
	case "fastapi", "fastapi.responses", "starlette.responses", "sse_starlette", "sse_starlette.sse":
	default:
		return "", false
	}
	ct, ok := responseClasses[short]
	return ct, ok
}

// modelEntry is one repository class resolved as a model.
type modelEntry struct {
	name   string
	model  routeshape.Model
	refs   []string // identities of the models its schema refers to
	failed bool
	// byName: populate_by_name or validate_by_name is set (inherited);
	// aliased: a field has an alias (row SH-F22); file: a field is an
	// UploadFile (a form model with it is not read, row SH-F20).
	byName, aliased, file bool
}

// typeContext carries the route-independent caches of the resolver.
type typeContext struct {
	files   map[string]facts.Facts
	modules *python.ModuleResolver
	sec     *securityLinker
	// models holds every class resolved as a model, by identity; names
	// maps a model name to its identity (names are unique).
	models map[string]*modelEntry
	names  map[string]string
	// building guards recursive models.
	building map[string]bool
}

func newTypeContext(files map[string]facts.Facts, modules *python.ModuleResolver, sec *securityLinker) *typeContext {
	return &typeContext{files: files, modules: modules, sec: sec, models: map[string]*modelEntry{}, names: map[string]string{}, building: map[string]bool{}}
}

// resolve resolves an annotation written in file/scope. refs collects the
// identities of the models the schema refers to.
func (t *typeContext) resolve(file string, scope int, e facts.RouteExpr, depth int, refs *[]string) typeInfo {
	if depth > routeshape.MaxSchemaDepth-2 {
		return unknownType("nested deeper than %d", routeshape.MaxSchemaDepth-2)
	}
	switch e.Kind {
	case "none":
		return typeInfo{schema: routeshape.Schema{Type: routeshape.TypeNull}, cat: catNone}
	case "literal":
		// A string annotation is a forward reference: read it as a name
		// when it is one.
		if isDotted(e.Name) {
			return t.resolve(file, scope, dotted(e.Name), depth+1, refs)
		}
		return unknownType("string annotation %q is not read", e.Name)
	case "identifier", "attribute":
		return t.named(file, scope, e, depth, refs)
	case "subscript":
		return t.generic(file, scope, e, depth, refs)
	case "union":
		return t.union(file, scope, e.Args, depth, refs)
	}
	return unknownType("annotation %s is not read", describe(e))
}

func describe(e facts.RouteExpr) string {
	if e.Text != "" {
		return e.Text
	}
	if e.Name != "" {
		return e.Name
	}
	return e.Kind
}

func isDotted(s string) bool {
	if s == "" {
		return false
	}
	for _, part := range strings.Split(s, ".") {
		if part == "" {
			return false
		}
		for i, r := range part {
			if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && r >= '0' && r <= '9') {
				return false
			}
		}
	}
	return true
}

// dotted builds the identifier/attribute expression of a dotted name.
func dotted(name string) facts.RouteExpr {
	parts := strings.Split(name, ".")
	e := facts.RouteExpr{Kind: "identifier", Name: parts[0], Text: parts[0]}
	for _, p := range parts[1:] {
		prev := e
		e = facts.RouteExpr{Kind: "attribute", Name: p, Object: &prev, Text: prev.Text + "." + p}
	}
	return e
}

func (t *typeContext) named(file string, scope int, e facts.RouteExpr, depth int, refs *[]string) typeInfo {
	b := t.modules.Expression(file, scope, e)
	if b.External {
		name := fullName(b)
		if s, ok := externalSpecial[name]; ok {
			tagShape("SH-F14/" + s)
			return typeInfo{cat: catSpecial, special: s, schema: routeshape.Schema{Type: routeshape.TypeUnknown, Reason: "framework value"}}
		}
		if externalFiles[name] {
			return typeInfo{schema: routeshape.Schema{Type: routeshape.TypeString}, cat: catFile}
		}
		if externalStrings[name] {
			return typeInfo{schema: routeshape.Schema{Type: routeshape.TypeString}, cat: catScalar, loose: looseStrings[name]}
		}
		switch name {
		case "typing.Any", "typing_extensions.Any":
			return typeInfo{schema: routeshape.Schema{Type: routeshape.TypeUnknown, Reason: "typed Any"}, cat: catScalar, loose: true}
		case "typing.List", "typing.Set", "typing.FrozenSet", "typing.Sequence", "collections.abc.Sequence":
			return builtinTypes["list"]
		case "typing.Dict", "typing.Mapping", "collections.abc.Mapping":
			return builtinTypes["dict"]
		}
		if _, ok := responseClass(b); ok {
			return typeInfo{cat: catSpecial, special: "Response", schema: routeshape.Schema{Type: routeshape.TypeUnknown, Reason: "a Response class"}}
		}
		return unknownType("%s is not read", name)
	}
	if b.File == "" && b.Module == nil && e.Kind == "identifier" {
		if bt, ok := builtinTypes[e.Name]; ok {
			return bt
		}
		return unknownType("%s is not resolved", e.Name)
	}
	if b.File == "" || b.Module != nil {
		return unknownType("%s is not resolved", describe(e))
	}
	switch b.Syntax.Value.Kind {
	case "class_definition":
		if b.Syntax.Count != 1 {
			return unknownType("%s is defined more than once", b.Name)
		}
		return t.class(b, depth, refs)
	case "subscript":
		// A module-level alias X = Annotated[...] or X = list[int].
		if b.Scope == 0 && b.Syntax.Stable {
			r := t.resolve(b.File, 0, b.Syntax.Value, depth+1, refs)
			r.alias = true
			return r
		}
	}
	return unknownType("%s is not a class", describe(e))
}

// baseNames resolves a class's bases to their full external names, or
// repository class bindings.
func (t *typeContext) bases(b python.Binding) ([]string, []python.Binding) {
	var ext []string
	var repo []python.Binding
	for _, base := range b.Syntax.Bases {
		if base.Kind == "keyword_argument" {
			continue
		}
		if base.Kind != "identifier" && base.Kind != "attribute" {
			if base.Kind == "subscript" {
				ext = append(ext, "generic")
			}
			continue
		}
		r := t.modules.Expression(b.File, b.Scope, base)
		switch {
		case r.External:
			ext = append(ext, fullName(r))
		case r.File != "" && r.Module == nil && r.Syntax.Value.Kind == "class_definition":
			repo = append(repo, r)
		case r.File == "" && r.Module == nil && base.Kind == "identifier":
			ext = append(ext, "builtins."+base.Name)
		default:
			ext = append(ext, "unresolved")
		}
	}
	return ext, repo
}

// root classifies a class by what its bases reach: "model" (Pydantic
// BaseModel or SQLModel), "enum", or "" (anything else).
func (t *typeContext) root(b python.Binding, seen map[string]bool) string {
	key := b.File + "#" + b.Identity
	if seen[key] {
		return ""
	}
	seen[key] = true
	ext, repo := t.bases(b)
	found := ""
	for _, name := range ext {
		switch name {
		case "pydantic.BaseModel", "pydantic.main.BaseModel", "sqlmodel.SQLModel", "sqlmodel.main.SQLModel":
			found = "model"
		case "enum.Enum", "enum.IntEnum", "enum.StrEnum":
			found = "enum"
		}
	}
	if found != "" {
		return found
	}
	for _, r := range repo {
		if f := t.root(r, seen); f != "" {
			return f
		}
	}
	return ""
}

// shapeClass finds a class's captured members.
func (t *typeContext) shapeClass(b python.Binding) ([]facts.PythonModelMember, bool) {
	p := t.files[b.File].Python
	if p == nil {
		return nil, false
	}
	if p.Shape != nil {
		for _, c := range p.Shape.Classes {
			if c.Scope == b.Syntax.ClassScope {
				return c.Members, true
			}
		}
	}
	return nil, false
}

func (t *typeContext) class(b python.Binding, depth int, refs *[]string) typeInfo {
	switch t.root(b, map[string]bool{}) {
	case "enum":
		return t.enum(b)
	case "model":
		name, ok := t.model(b)
		if !ok {
			return unknownType("model %s is not read", b.Name)
		}
		*refs = append(*refs, b.Identity)
		return typeInfo{schema: routeshape.Schema{Type: routeshape.TypeRef, Ref: name}, cat: catModel}
	}
	return unknownType("class %s is not a Pydantic model or an enum", b.Name)
}

func (t *typeContext) enum(b python.Binding) typeInfo {
	members, ok := t.shapeClass(b)
	if !ok {
		return unknownType("enum %s: members not read", b.Name)
	}
	var values []any
	for _, m := range members {
		switch m.Kind {
		case "assign":
			if strings.HasPrefix(m.Name, "_") {
				continue
			}
			if m.Value == nil || m.Value.Kind != "literal" {
				return unknownType("enum %s: member %s is not a literal", b.Name, m.Name)
			}
			v, ok := literalValue(*m.Value)
			if !ok {
				return unknownType("enum %s: member %s is not a literal", b.Name, m.Name)
			}
			values = append(values, v)
		case "method":
			if m.Name == "_missing_" {
				tagShape("SH-F8/_missing_")
				return unknownType("enum %s defines _missing_: other values may be accepted", b.Name)
			}
		default:
			return unknownType("enum %s: a member is not read", b.Name)
		}
	}
	if len(values) == 0 {
		return unknownType("enum %s has no members read", b.Name)
	}
	tagShape("SH-F8/enum")
	return typeInfo{schema: routeshape.Schema{Type: routeshape.TypeEnum, Enum: values}, cat: catScalar, enum: true}
}

func literalValue(e facts.PythonModelExpr) (any, bool) {
	switch e.LitKind {
	case "str":
		return e.Literal, true
	case "int":
		n, err := strconv.ParseInt(e.Literal, 0, 64)
		return n, err == nil
	case "float":
		f, err := strconv.ParseFloat(e.Literal, 64)
		return f, err == nil
	case "bool":
		return e.Literal == "True", true
	case "none":
		return nil, true
	}
	return nil, false
}

// generic resolves a subscript annotation.
func (t *typeContext) generic(file string, scope int, e facts.RouteExpr, depth int, refs *[]string) typeInfo {
	if e.Object == nil {
		return unknownType("annotation is not read")
	}
	base := ""
	b := t.modules.Expression(file, scope, *e.Object)
	switch {
	case b.External:
		base = fullName(b)
	case b.File == "" && b.Module == nil && e.Object.Kind == "identifier":
		base = "builtins." + e.Object.Name
	default:
		return unknownType("generic %s is not read", describe(*e.Object))
	}
	args := e.Args
	if len(args) == 1 && args[0].Kind == "tuple" {
		args = args[0].Args
	}
	item := func() typeInfo {
		if len(args) != 1 {
			return unknownType("%s with %d arguments is not read", base, len(args))
		}
		it := t.resolve(file, scope, args[0], depth+1, refs)
		s := it.schema
		cat := catSequence
		if it.cat == catFile {
			// A sequence of UploadFile is a file field (FastAPI's
			// is_uploadfile_sequence_annotation, row SH-F20).
			tagShape("SH-F20/list[UploadFile]")
			cat = catFile
		}
		return typeInfo{schema: routeshape.Schema{Type: routeshape.TypeArray, Items: &s}, cat: cat, loose: it.loose}
	}
	if streamTypes[base] {
		return typeInfo{schema: routeshape.Schema{Type: routeshape.TypeUnknown, Reason: base + " is streamed item by item"}, cat: catUnknown, stream: true}
	}
	switch base {
	case "typing.Annotated", "typing_extensions.Annotated":
		if len(args) == 0 {
			return unknownType("empty Annotated")
		}
		constrained := false
		for _, m := range args[1:] {
			ok, constraint := t.metadata(file, scope, m)
			if !ok {
				tagShape("SH-F22/metadata")
				return unknownType("Annotated metadata %s is not read (it may change what is accepted)", describe(m))
			}
			constrained = constrained || constraint
		}
		r := t.resolve(file, scope, args[0], depth+1, refs)
		if constrained {
			r.loose = false
		}
		return r
	case "typing.Optional":
		if len(args) != 1 {
			return unknownType("Optional with %d arguments", len(args))
		}
		return t.union(file, scope, []facts.RouteExpr{args[0], {Kind: "none"}}, depth, refs)
	case "typing.Union":
		return t.union(file, scope, args, depth, refs)
	case "builtins.list", "builtins.set", "builtins.frozenset", "typing.List", "typing.Set", "typing.FrozenSet", "typing.Sequence", "collections.abc.Sequence":
		return item()
	case "builtins.tuple", "typing.Tuple":
		if len(args) == 2 && args[1].Kind == "ellipsis" {
			args = args[:1]
			return item()
		}
		return unknownType("tuple with fixed items is not read")
	case "builtins.dict", "typing.Dict", "typing.Mapping", "collections.abc.Mapping":
		return typeInfo{schema: routeshape.Schema{Type: routeshape.TypeObject, Additional: routeshape.AdditionalAllowed}, cat: catMapping}
	case "typing.Literal", "typing_extensions.Literal":
		var values []any
		for _, a := range args {
			switch a.Kind {
			case "literal":
				values = append(values, a.Name)
			case "integer":
				n, err := strconv.ParseInt(a.Name, 0, 64)
				if err != nil {
					return unknownType("Literal value %s is not read", a.Name)
				}
				values = append(values, n)
			case "true", "false":
				values = append(values, a.Kind == "true")
			case "none":
				values = append(values, nil)
			default:
				return unknownType("Literal value %s is not read", describe(a))
			}
		}
		if len(values) == 0 {
			return unknownType("empty Literal")
		}
		tagShape("SH-F8/literal")
		return typeInfo{schema: routeshape.Schema{Type: routeshape.TypeEnum, Enum: values}, cat: catScalar, enum: true}
	}
	return unknownType("generic %s is not read", base)
}

// metadata reads one Annotated metadata item outside FastAPI's markers:
// ok when it keeps the type's schema (allowedMetadata or a literal),
// constraint when it rejects some values.
func (t *typeContext) metadata(file string, scope int, m facts.RouteExpr) (ok, constraint bool) {
	switch m.Kind {
	case "literal", "string", "integer", "float", "true", "false", "none":
		return true, false
	case "call":
		if m.Object == nil {
			return false, false
		}
		b := t.modules.Expression(file, scope, *m.Object)
		if !b.External || !allowedMetadata(fullName(b)) {
			return false, false
		}
		return true, constraintMetadata(fullName(b), m)
	}
	return false, false
}

// union resolves T | None (nullable T); any other union is unknown.
func (t *typeContext) union(file string, scope int, args []facts.RouteExpr, depth int, refs *[]string) typeInfo {
	var rest []typeInfo
	nullable := false
	for _, a := range args {
		r := t.resolve(file, scope, a, depth+1, refs)
		if r.cat == catNone {
			nullable = true
			continue
		}
		rest = append(rest, r)
	}
	if len(rest) != 1 {
		return unknownType("a union of %d types is not read", len(rest))
	}
	r := rest[0]
	if nullable {
		if r.cat == catSpecial {
			return r
		}
		r.schema.Nullable = true
	}
	return r
}

// model builds (once) the model of a repository class reaching BaseModel
// or SQLModel (row SH-F22) and returns its name.
func (t *typeContext) model(b python.Binding) (string, bool) {
	if m, ok := t.models[b.Identity]; ok {
		return m.name, !m.failed
	}
	name := b.Name
	if other, taken := t.names[name]; taken && other != b.Identity {
		// Another class has the name: qualify it with its file.
		q := strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
				return r
			}
			return '_'
		}, strings.TrimSuffix(b.File, ".py"))
		name = b.Name + "__" + q
		if t.names[name] != "" {
			name = fmt.Sprintf("%s__%d", b.Name, len(t.names))
		}
	}
	entry := &modelEntry{name: name}
	t.models[b.Identity] = entry
	t.names[name] = b.Identity
	schema, refs := t.modelSchema(b)
	entry.refs = refs
	line := 1
	if p := t.files[b.File].Python; p != nil && p.Shape != nil {
		for _, c := range p.Shape.Classes {
			if c.Scope == b.Syntax.ClassScope {
				line = c.Line
			}
		}
	}
	tagShape("SH-F22")
	// The class (and the repository bases modelFields read) is the
	// declaration the model was read from (row MT-4).
	if b.Identity != "" && !containsString(schema.Decls, b.Identity) {
		schema.Decls = append(schema.Decls, b.Identity)
	}
	entry.model = routeshape.Model{Name: name, Schema: schema, Provenance: routeshape.Provenance{Kind: routeshape.Fact, Rule: "SH-F22", File: b.File, Line: line, Confidence: 1}}
	return name, true
}

// modelSchema reads a model class's fields: its repository model bases
// first, then its own annotated members.
func (t *typeContext) modelSchema(b python.Binding) (routeshape.Schema, []string) {
	obj, refs := t.modelFields(b)
	if e := t.models[b.Identity]; e != nil && obj.Type == routeshape.TypeObject && e.byName && e.aliased {
		// Either name of an aliased field validates: its presence under
		// the alias is not what FastAPI checks.
		tagShape("SH-F22/populate_by_name")
		return routeshape.Schema{Type: routeshape.TypeUnknown, Reason: "populate_by_name: an aliased field is also accepted under its field name"}, nil
	}
	return obj, refs
}

// modelFields reads a model class's fields (modelSchema without the
// populate_by_name check).
func (t *typeContext) modelFields(b python.Binding) (routeshape.Schema, []string) {
	var refs []string
	entry := t.models[b.Identity]
	if entry == nil {
		entry = &modelEntry{}
	}
	unknown := func(format string, args ...any) (routeshape.Schema, []string) {
		return routeshape.Schema{Type: routeshape.TypeUnknown, Reason: fmt.Sprintf(format, args...)}, nil
	}
	ext, repo := t.bases(b)
	for _, name := range ext {
		switch name {
		case "pydantic.BaseModel", "pydantic.main.BaseModel", "sqlmodel.SQLModel", "sqlmodel.main.SQLModel":
		default:
			return unknown("base %s is not read", name)
		}
	}
	for _, k := range keywordsOf(b) {
		// table=True: SQLModel table classes do not validate as plain
		// models; any other keyword is not read.
		return unknown("class keyword %s is not read", k)
	}
	if len(repo) > 1 || len(repo) == 1 && len(ext) > 0 {
		return unknown("several bases are not read")
	}
	obj := routeshape.Schema{Type: routeshape.TypeObject, Additional: routeshape.AdditionalUnknown}
	fieldAt := map[string]int{}
	if len(repo) == 1 {
		baseName, ok := t.model(repo[0])
		if !ok {
			return unknown("base %s is not read", repo[0].Name)
		}
		base := t.models[repo[0].Identity]
		if base.model.Schema.Type != routeshape.TypeObject {
			return unknown("base %s is not read", baseName)
		}
		obj.Additional = base.model.Schema.Additional
		entry.byName, entry.aliased, entry.file = base.byName, base.aliased, base.file
		for _, f := range base.model.Schema.Fields {
			fieldAt[f.Name] = len(obj.Fields)
			obj.Fields = append(obj.Fields, f)
		}
		refs = append(refs, base.refs...)
		obj.Decls = append(obj.Decls, base.model.Schema.Decls...)
	}
	members, ok := t.shapeClass(b)
	if !ok {
		return unknown("class body of %s is not read", b.Name)
	}
	for _, m := range members {
		switch m.Kind {
		case "annotated":
			if m.Annotation == nil || strings.HasPrefix(m.Name, "_") {
				continue
			}
			ann := modelToRoute(*m.Annotation)
			if ann.Kind == "subscript" && ann.Object != nil && (ann.Object.Name == "ClassVar") {
				continue
			}
			field, skip, problem := t.field(b, m, ann, &refs)
			if problem != "" {
				return unknown("field %s: %s", m.Name, problem)
			}
			if skip {
				continue
			}
			if field.Name != m.Name {
				entry.aliased = true
			}
			if i, dup := fieldAt[field.Name]; dup {
				obj.Fields[i] = field
				continue
			}
			fieldAt[field.Name] = len(obj.Fields)
			obj.Fields = append(obj.Fields, field)
		case "assign":
			if m.Name == "model_config" {
				extra, byName, ok := configExtra(m.Value)
				if !ok {
					return unknown("model_config is not read")
				}
				if extra != "" {
					obj.Additional = extra
				}
				entry.byName = entry.byName || byName
			}
		case "class":
			if m.Name == "Config" {
				for _, c := range m.Members {
					switch {
					case c.Kind == "assign" && c.Name == "extra":
						extra, ok := extraValue(c.Value)
						if !ok {
							return unknown("Config.extra is not read")
						}
						obj.Additional = extra
					case c.Kind == "assign" && byNameConfig[c.Name]:
						entry.byName = entry.byName || !literalFalse(c.Value)
					case c.Kind == "assign" && neutralConfig[c.Name]:
					default:
						return unknown("Config.%s is not read", c.Name)
					}
				}
			}
		case "method":
			for _, d := range m.Decorators {
				switch lastDotted(d.Name) {
				case "computed_field":
					return unknown("computed field %s is not read", m.Name)
				case "field_serializer", "model_serializer", "model_validator", "root_validator", "pre_init":
					// Serializers change the output, model validators can
					// rewrite the input before the fields are checked.
					return unknown("%s %s is not read", lastDotted(d.Name), m.Name)
				case "field_validator", "validator":
					// A before, wrap or plain validator (v1: pre=True)
					// replaces the field's own validation.
					if replacingValidator(d) {
						tagShape("SH-F22/validator")
						return unknown("%s %s runs before the field's validation", lastDotted(d.Name), m.Name)
					}
				}
			}
		}
	}
	return obj, refs
}

func lastDotted(s string) string { return s[strings.LastIndex(s, ".")+1:] }

// keywordsOf lists the class keywords (table=True, metaclass=...): the
// cached bases keep them as keyword_argument expressions.
func keywordsOf(b python.Binding) []string {
	var out []string
	for _, base := range b.Syntax.Bases {
		if base.Kind == "keyword_argument" {
			name, _, _ := strings.Cut(base.Text, "=")
			out = append(out, strings.TrimSpace(name))
		}
	}
	return out
}

func configExtra(v *facts.PythonModelExpr) (routeshape.Additional, bool, bool) {
	if v == nil || v.Kind != "call" || lastDotted(v.Name) != "ConfigDict" && lastDotted(v.Name) != "SQLModelConfig" {
		return "", false, false
	}
	extra := routeshape.Additional("")
	byName := false
	for _, k := range v.Keywords {
		switch {
		case k.Name == "extra":
			e, ok := extraValue(&k.Value)
			if !ok {
				return "", false, false
			}
			extra = e
		case byNameConfig[k.Name]:
			value := k.Value
			byName = byName || !literalFalse(&value)
		case !neutralConfig[k.Name]:
			// alias_generator, serialization settings, ...
			// change names or the schema: not read.
			return "", false, false
		}
	}
	return extra, byName, true
}

// byNameConfig are the configuration keys that let a field be filled
// under its field name as well as its alias (Pydantic 2.14).
var byNameConfig = map[string]bool{"populate_by_name": true, "validate_by_name": true}

func literalFalse(v *facts.PythonModelExpr) bool {
	return v != nil && v.Kind == "literal" && v.LitKind == "bool" && v.Literal == "False"
}

// replacingValidator reports a field_validator whose mode is not a
// literal "after", or a v1 validator with pre=True (or not a literal).
func replacingValidator(d facts.PythonModelExpr) bool {
	if d.Kind != "call" {
		return false
	}
	for _, k := range d.Keywords {
		switch {
		case k.Name == "":
			return true
		case lastDotted(d.Name) == "field_validator" && k.Name == "mode":
			return !(k.Value.Kind == "literal" && k.Value.LitKind == "str" && k.Value.Literal == "after")
		case lastDotted(d.Name) == "validator" && k.Name == "pre":
			return !literalFalse(&k.Value)
		}
	}
	return false
}

// neutralConfig are model configuration keys that change neither field
// names, presence nor types of the JSON schema (Pydantic 2.14).
var neutralConfig = map[string]bool{
	"from_attributes": true, "orm_mode": true, "arbitrary_types_allowed": true, "frozen": true,
	"validate_assignment": true, "str_strip_whitespace": true, "use_enum_values": true,
	"protected_namespaces": true, "validate_default": true, "title": true, "table": true,
	"json_schema_extra": true,
}

func extraValue(v *facts.PythonModelExpr) (routeshape.Additional, bool) {
	if v == nil || v.Kind != "literal" || v.LitKind != "str" {
		return "", false
	}
	switch v.Literal {
	case "forbid":
		return routeshape.AdditionalForbidden, true
	case "allow":
		return routeshape.AdditionalAllowed, true
	case "ignore":
		return routeshape.AdditionalUnknown, true
	}
	return "", false
}

// field reads one annotated member: its name (alias), presence (default)
// and schema. skip is true for members that are not fields
// (Relationship); problem names a form not read.
func (t *typeContext) field(b python.Binding, m facts.PythonModelMember, ann facts.RouteExpr, refs *[]string) (routeshape.Field, bool, string) {
	f := routeshape.Field{Name: m.Name, Presence: routeshape.Required}
	// implicit: no default is written (neither a value nor `...`).
	// Pydantic 2 requires such a field; Pydantic 1 makes it optional
	// when its type accepts None.
	implicit := true
	if ann.Kind == "subscript" && ann.Object != nil && ann.Object.Name == "Annotated" {
		for _, meta := range ann.Args[1:] {
			if meta.Kind == "call" {
				return f, false, "Annotated metadata calls are not read"
			}
		}
	}
	if v := m.Value; v != nil {
		switch {
		case v.Kind == "call" && lastDotted(v.Name) == "Relationship":
			return f, true, ""
		case v.Kind == "call" && lastDotted(v.Name) == "Field":
			present := false
			for i, a := range v.Args {
				if i == 0 {
					present = !(a.Kind == "literal" && a.LitKind == "ellipsis")
					implicit = false
				}
			}
			for _, k := range v.Keywords {
				switch k.Name {
				case "":
					return f, false, "Field(**...) is not read"
				case "default":
					present = !(k.Value.Kind == "literal" && k.Value.LitKind == "ellipsis")
					implicit = false
				case "default_factory":
					present, implicit = true, false
				case "alias":
					if k.Value.Kind != "literal" || k.Value.LitKind != "str" {
						return f, false, "alias is not a literal"
					}
					f.Name = k.Value.Literal
				case "validation_alias", "serialization_alias", "alias_priority", "exclude", "discriminator", "init", "init_var", "sa_column":
					return f, false, k.Name + "= is not read"
				}
			}
			if present {
				f.Presence = routeshape.Optional
			}
		case v.Kind == "literal" && v.LitKind != "ellipsis":
			f.Presence = routeshape.Optional
		case v.Kind == "literal" && v.LitKind == "ellipsis":
			implicit = false
		default:
			// Any other default value makes the field optional.
			f.Presence = routeshape.Optional
		}
	}
	r := t.resolve(b.File, b.Scope, ann, 1, refs)
	if r.cat == catSpecial {
		return f, false, "a framework value is not a field"
	}
	if r.cat == catFile {
		if e := t.models[b.Identity]; e != nil {
			e.file = true
		}
	}
	f.Schema = r.schema
	if f.Presence == routeshape.Required && implicit && mayBeNone(f.Schema) && !t.pydantic2(b.File) {
		// Pydantic 1 gives a field that accepts None, or whose type is
		// not read, a None default; Pydantic 2 requires it. Without a
		// proven major version, whether it must be sent is unknown.
		tagShape("SH-F22/pydantic version")
		f.Presence = routeshape.PresenceUnknown
	}
	return f, false, ""
}

// pydantic2Version is the first release that requires a field without a
// default even when its type accepts None.
var pydantic2Version = python.Version{2, 0, 0}

// pydantic2 reports whether the manifest enclosing file proves that every
// allowed Pydantic install is 2 or later (row SH-F22).
func (t *typeContext) pydantic2(file string) bool {
	v, _, ok := t.modules.PydanticMinimum(file)
	return ok && v.AtLeast(pydantic2Version)
}

// mayBeNone reports whether a field of this schema may accept None:
// nullable, None itself, or a type that is not read.
func mayBeNone(s routeshape.Schema) bool {
	return s.Nullable || s.Type == routeshape.TypeNull || s.Type == routeshape.TypeUnknown
}

// modelToRoute converts a model-capture expression to the RouteExpr form
// the resolver reads.
func modelToRoute(e facts.PythonModelExpr) facts.RouteExpr {
	switch e.Kind {
	case "name":
		if isDotted(e.Name) {
			return dotted(e.Name)
		}
	case "literal":
		switch e.LitKind {
		case "str":
			return facts.RouteExpr{Kind: "literal", Name: e.Literal}
		case "int":
			return facts.RouteExpr{Kind: "integer", Name: e.Literal}
		case "bool":
			if e.Literal == "True" {
				return facts.RouteExpr{Kind: "true"}
			}
			return facts.RouteExpr{Kind: "false"}
		case "none":
			return facts.RouteExpr{Kind: "none"}
		case "ellipsis":
			return facts.RouteExpr{Kind: "ellipsis"}
		}
	case "subscript":
		if isDotted(e.Name) {
			o := dotted(e.Name)
			out := facts.RouteExpr{Kind: "subscript", Object: &o, Text: e.Text}
			for _, a := range e.Args {
				out.Args = append(out.Args, modelToRoute(a))
			}
			return out
		}
	case "union":
		out := facts.RouteExpr{Kind: "union", Text: e.Text}
		for _, a := range e.Args {
			out.Args = append(out.Args, modelToRoute(a))
		}
		return out
	case "tuple", "list":
		out := facts.RouteExpr{Kind: e.Kind}
		for _, a := range e.Args {
			out.Args = append(out.Args, modelToRoute(a))
		}
		return out
	case "call":
		return facts.RouteExpr{Kind: "call", Text: e.Name}
	}
	return facts.RouteExpr{Kind: "other", Text: e.Text}
}

// routeModels returns the models a shape refers to (transitively), in a
// stable order.
func (t *typeContext) routeModels(refs []string) []routeshape.Model {
	seen := map[string]bool{}
	var out []routeshape.Model
	var add func(id string)
	add = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		m := t.models[id]
		if m == nil {
			return
		}
		out = append(out, m.model)
		for _, r := range m.refs {
			add(r)
		}
	}
	for _, r := range refs {
		add(r)
	}
	return out
}

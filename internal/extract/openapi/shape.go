package openapi

import (
	"fmt"
	"math/big"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"encoding/json"

	"github.com/graphvein/graphvein/internal/extract/routeshape"
)

// This file reads the committed spec operation of a linked route into
// mapped shape items (T029, rows SH-O1..SH-O19 of
// docs/contract/claims/route-shape-spec.md). Nothing here is a fact: the
// spec is shown beside what the code proves, never instead of it, and a
// contradiction between the two is written as a "spec differs" note.

// specShape is the mapped part of a route shape read from one committed
// operation, before the confidence of the route's link is set.
type specShape struct {
	params   map[string][]routeshape.Param
	body     []routeshape.Content
	bodyNote string
	// notes holds extra reason notes by section name: fields marked
	// readOnly or writeOnly (SH-O12) and response keys not stored (SH-O8).
	notes     map[string]string
	responses []routeshape.Response
	security  []routeshape.Requirement
	schemes   []routeshape.Scheme
	models    []routeshape.Model
}

// shapeMapper converts one operation. models maps a model name to its
// index in out.models, or -1 while the model is being built (a cycle).
//
// The readOnly/writeOnly bookkeeping finds, per section, whether a field
// so marked is reachable from the section's items (inline or through the
// models they reference), so the section's reason can say why those
// fields have presence unknown (SH-O12). section is the section being
// read and model the model being built ("" for an inline schema).
type shapeMapper struct {
	p       *pass
	root    *document
	swagger bool
	minor   int
	models  map[string]int
	out     *specShape

	section   string
	model     string
	marksIn   map[string]rwMarks  // section -> marks on inline fields
	marksOf   map[string]rwMarks  // model -> marks on its own fields
	refsIn    map[string][]string // section -> models referenced inline
	refsOf    map[string][]string // model -> models it references
	responseN []string            // response keys not stored (SH-O8)
}

// rwMarks records readOnly (1) and writeOnly (2) marks.
type rwMarks int

const (
	markReadOnly rwMarks = 1 << iota
	markWriteOnly
)

// annotations are the schema keywords that never change which values a
// schema accepts; a $ref (3.1) or a single allOf member keeps its meaning
// beside them.
var annotations = map[string]bool{"description": true, "summary": true, "title": true, "example": true, "examples": true,
	"deprecated": true, "readOnly": true, "writeOnly": true, "default": true, "$comment": true, "externalDocs": true, "xml": true}

func onlyAnnotations(v *value, except ...string) bool {
	for _, e := range v.fields() {
		if annotations[e.key] || strings.HasPrefix(e.key, "x-") || contains(except, e.key) {
			continue
		}
		return false
	}
	return true
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// line returns the 1-based line of a byte offset of the document.
func (d *document) line(offset int) int {
	if d.lines == nil {
		d.lines = []int{0}
		for i, b := range d.source {
			if b == '\n' {
				d.lines = append(d.lines, i+1)
			}
		}
	}
	return sort.Search(len(d.lines), func(i int) bool { return d.lines[i] > offset })
}

// lineOf is the line of a location: its key in the document, or for a
// sequence item its first key.
func lineOf(loc location) int {
	if loc.doc == nil || loc.doc.root == nil {
		return 1
	}
	if e, ok := pointerEntry(loc.doc.root, loc.pointer); ok && e.start > 0 {
		return loc.doc.line(e.start)
	}
	if loc.value != nil {
		for _, f := range loc.value.fields() {
			return loc.doc.line(f.start)
		}
	}
	return 1
}

func (m *shapeMapper) provenance(form string, loc location) routeshape.Provenance {
	return routeshape.Provenance{Kind: routeshape.Mapped, Rule: rowOf(form), File: loc.doc.file, Line: lineOf(loc), Chain: []routeshape.Step{}, Confidence: 1}
}

// mapOperation reads one operation (rows SH-O4..SH-O17). item is the path
// item that holds it, op the operation itself.
func (p *pass) mapOperation(d *document, item, op location) *specShape {
	p.quiet = true
	defer func() { p.quiet = false }()
	m := &shapeMapper{p: p, root: d, swagger: d.format == "swagger", minor: d.minor, models: map[string]int{},
		out:     &specShape{params: map[string][]routeshape.Param{}, notes: map[string]string{}},
		marksIn: map[string]rwMarks{}, marksOf: map[string]rwMarks{}, refsIn: map[string][]string{}, refsOf: map[string][]string{}}
	m.parameters(item, op)
	if !m.swagger {
		m.section = routeshape.SectionBody
		m.requestBody(op)
	}
	m.section = routeshape.SectionResponses
	m.responses(op)
	m.securityRequirements(op)
	for _, sec := range routeshape.SectionNames {
		m.out.notes[sec] = joinNotes(m.marksNote(sec), m.out.notes[sec])
	}
	if len(m.responseN) > 0 {
		m.out.notes[routeshape.SectionResponses] = joinNotes(m.out.notes[routeshape.SectionResponses],
			"the committed spec's response keys "+strings.Join(m.responseN, ", ")+" are not Swagger 2.0 status codes: not stored")
	}
	return m.out
}

// marksNote says why fields marked readOnly or writeOnly that a section
// reaches have presence unknown (SH-O12): the shape has one view of a
// schema for requests and responses, so neither direction's presence is
// claimed.
func (m *shapeMapper) marksNote(section string) string {
	marks := m.marksIn[section]
	seen := map[string]bool{}
	queue := append([]string(nil), m.refsIn[section]...)
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if seen[name] {
			continue
		}
		seen[name] = true
		marks |= m.marksOf[name]
		queue = append(queue, m.refsOf[name]...)
	}
	var notes []string
	if marks&markReadOnly != 0 {
		notes = append(notes, "fields the committed spec marks readOnly (not sent in requests) have presence unknown")
	}
	if marks&markWriteOnly != 0 {
		notes = append(notes, "fields the committed spec marks writeOnly (not sent in responses) have presence unknown")
	}
	return joinNotes(notes...)
}

func unknownSchema(form, reason string) routeshape.Schema {
	rowOf(form)
	return routeshape.Schema{Type: routeshape.TypeUnknown, Reason: reason}
}

// parameters reads the merged path item and operation parameters (SH-O4,
// SH-O5, SH-O6): an operation parameter replaces the path item's of the
// same name and location.
func (m *shapeMapper) parameters(item, op location) {
	type key struct{ name, in string }
	var order []key
	found := map[key]location{}
	for _, owner := range []location{item, op} {
		for i, raw := range owner.value.get("parameters").list() {
			loc := m.p.resolve(location{owner.doc, raw, fmt.Sprintf("%s/parameters/%d", owner.pointer, i)})
			if loc.value == nil {
				continue
			}
			name, in := loc.value.get("name").text(), loc.value.get("in").text()
			if name == "" || in == "" {
				continue
			}
			k := key{name, in}
			if in == "header" {
				k.name = strings.ToLower(name)
			}
			if _, dup := found[k]; dup {
				if owner.value == op.value {
					rowOf("param:override")
				}
			} else {
				order = append(order, k)
			}
			found[k] = loc
		}
	}
	var form []routeshape.Field
	var formLoc location
	for _, k := range order {
		loc := found[k]
		v := loc.value
		name, in := v.get("name").text(), k.in
		m.section = in
		if m.swagger && (in == "body" || in == "formData") {
			m.section = routeshape.SectionBody
		}
		switch {
		case m.swagger && in == "body":
			m.swaggerBody(op, loc)
			continue
		case m.swagger && in == "formData":
			if form == nil {
				formLoc = loc
			}
			presence := routeshape.Optional
			if v.get("required").boolean() {
				presence = routeshape.Required
			}
			form = append(form, routeshape.Field{Name: name, Presence: presence, Schema: m.schema(loc, 1)})
			continue
		case in != "path" && in != "query" && in != "header" && (in != "cookie" || m.swagger):
			continue
		case !m.swagger && in == "header" && (k.name == "accept" || k.name == "content-type" || k.name == "authorization"):
			rowOf("param:reserved-header")
			continue
		}
		presence := routeshape.Optional
		if in == "path" || v.get("required").boolean() {
			presence = routeshape.Required
		}
		var schema routeshape.Schema
		pform := "param:" + in
		switch {
		case m.swagger:
			pform = "swagger:parameter"
			schema = m.schema(loc, 0)
		case v.get("schema") != nil:
			schema = m.schema(location{loc.doc, v.get("schema"), loc.pointer + "/schema"}, 0)
		case v.get("content") != nil:
			types := sortedKeys(v.get("content"))
			schema = unknownSchema("param:content", "serialized as "+strings.Join(types, ", ")+" in the spec: not modelled")
		default:
			schema = unknownSchema("schema:no-type", "no schema in the spec")
		}
		m.out.params[in] = append(m.out.params[in], routeshape.Param{Name: name, Access: routeshape.Declared, Presence: presence, Schema: &schema, Provenance: m.provenance(pform, loc)})
	}
	if form != nil {
		schema := routeshape.Schema{Type: routeshape.TypeObject, Fields: form, Additional: routeshape.AdditionalUnknown}
		types := []*string{}
		for _, ct := range m.consumes(op) {
			if ct == "application/x-www-form-urlencoded" || ct == "multipart/form-data" {
				c := ct
				types = append(types, &c)
			}
		}
		if len(types) == 0 {
			types = append(types, nil)
		}
		for _, ct := range types {
			m.addBody(routeshape.Content{ContentType: ct, Schema: copySchema(schema), Provenance: m.provenance("swagger:formData", formLoc)})
		}
	}
}

func copySchema(s routeshape.Schema) *routeshape.Schema { return &s }

// consumes is a Swagger operation's effective consumes list.
func (m *shapeMapper) consumes(op location) []string {
	v, exists := op.value.field("consumes")
	if !exists {
		v.value = m.root.root.get("consumes")
	}
	return stringsList(v.value)
}

// produces is a Swagger operation's effective produces list.
func (m *shapeMapper) produces(op location) []string {
	v, exists := op.value.field("produces")
	if !exists {
		v.value = m.root.root.get("produces")
	}
	return stringsList(v.value)
}

func (m *shapeMapper) addBody(c routeshape.Content) {
	key := func(c routeshape.Content) string {
		if c.ContentType == nil {
			return "\x00"
		}
		return *c.ContentType
	}
	for _, old := range m.out.body {
		if key(old) == key(c) {
			return
		}
	}
	m.out.body = append(m.out.body, c)
}

func (m *shapeMapper) bodyRequired(required bool) {
	if required {
		rowOf("body:required")
		m.out.bodyNote = "the committed spec requires a request body"
	} else {
		m.out.bodyNote = "the committed spec marks the request body optional"
	}
}

// swaggerBody reads a Swagger 2.0 body parameter (SH-O6): one body item
// per consumed content type.
func (m *shapeMapper) swaggerBody(op, loc location) {
	schema := m.schema(location{loc.doc, loc.value.get("schema"), loc.pointer + "/schema"}, 0)
	types := []*string{}
	for _, ct := range m.consumes(op) {
		c := ct
		types = append(types, &c)
	}
	if len(types) == 0 {
		types = append(types, nil)
	}
	for _, ct := range types {
		m.addBody(routeshape.Content{ContentType: ct, Schema: copySchema(schema), Provenance: m.provenance("swagger:body", loc)})
	}
	m.bodyRequired(loc.value.get("required").boolean())
}

// requestBody reads an OpenAPI 3 request body (SH-O7).
func (m *shapeMapper) requestBody(op location) {
	rb, exists := op.value.field("requestBody")
	if !exists {
		return
	}
	loc := m.p.resolve(location{op.doc, rb.value, op.pointer + "/requestBody"})
	if loc.value == nil {
		return
	}
	for _, e := range loc.value.get("content").fields() {
		mt := location{loc.doc, e.value, loc.pointer + "/content/" + escape(e.key)}
		ct := e.key
		var schema routeshape.Schema
		if s := e.value.get("schema"); s != nil {
			schema = m.schema(location{mt.doc, s, mt.pointer + "/schema"}, 0)
		} else {
			schema = unknownSchema("body:no-schema", "no schema in the spec")
		}
		m.addBody(routeshape.Content{ContentType: &ct, Schema: &schema, Provenance: m.provenance("body:content", mt)})
	}
	m.bodyRequired(loc.value.get("required").boolean())
}

var (
	statusCode  = regexp.MustCompile(`^[1-5][0-9][0-9]$`)
	statusRange = regexp.MustCompile(`^[1-5][xX][xX]$`)
)

// responses reads the documented responses (SH-O8).
func (m *shapeMapper) responses(op location) {
	seen := map[string]bool{}
	add := func(r routeshape.Response) {
		key := r.Status + "\x00"
		if r.ContentType != nil {
			key += *r.ContentType
		}
		if !seen[key] {
			seen[key] = true
			m.out.responses = append(m.out.responses, r)
		}
	}
	for _, e := range op.value.get("responses").fields() {
		var status string
		switch {
		case statusCode.MatchString(e.key):
			status = e.key
			rowOf("response:status")
		case statusRange.MatchString(e.key) && m.swagger:
			// Swagger 2.0 defines status codes and default only.
			rowOf("response:swagger-range")
			m.responseN = append(m.responseN, e.key)
			continue
		case statusRange.MatchString(e.key):
			status = strings.ToUpper(e.key)
			rowOf("response:range")
		case e.key == "default":
			status = e.key
			rowOf("response:default")
		default:
			continue
		}
		loc := m.p.resolve(location{op.doc, e.value, op.pointer + "/responses/" + escape(e.key)})
		if loc.value == nil {
			continue
		}
		null := func() *routeshape.Schema { return &routeshape.Schema{Type: routeshape.TypeNull} }
		if m.swagger {
			s := loc.value.get("schema")
			if s == nil {
				add(routeshape.Response{Status: status, Schema: null(), Provenance: m.provenance("response:swagger", loc)})
				continue
			}
			schema := m.schema(location{loc.doc, s, loc.pointer + "/schema"}, 0)
			types := m.produces(op)
			if len(types) == 0 {
				add(routeshape.Response{Status: status, Schema: copySchema(schema), Provenance: m.provenance("response:swagger", loc)})
			}
			for _, ct := range types {
				c := ct
				add(routeshape.Response{Status: status, ContentType: &c, Schema: copySchema(schema), Provenance: m.provenance("response:swagger", loc)})
			}
			continue
		}
		content := loc.value.get("content").fields()
		if len(content) == 0 {
			add(routeshape.Response{Status: status, Schema: null(), Provenance: m.provenance("response:no-content", loc)})
			continue
		}
		for _, c := range content {
			mt := location{loc.doc, c.value, loc.pointer + "/content/" + escape(c.key)}
			ct := c.key
			var schema routeshape.Schema
			if s := c.value.get("schema"); s != nil {
				schema = m.schema(location{mt.doc, s, mt.pointer + "/schema"}, 0)
			} else {
				schema = unknownSchema("body:no-schema", "no schema in the spec")
			}
			add(routeshape.Response{Status: status, ContentType: &ct, Schema: &schema, Provenance: m.provenance("response:content", mt)})
		}
	}
}

// securityRequirements reads the operation's security, else the root's
// (SH-O16), and the schemes they name (SH-O17).
func (m *shapeMapper) securityRequirements(op location) {
	sec, exists := op.value.field("security")
	loc := location{op.doc, sec.value, op.pointer + "/security"}
	form := "security:operation"
	if !exists {
		sec, exists = m.root.root.field("security")
		loc = location{m.root, sec.value, "/security"}
		form = "security:root"
	}
	if !exists || sec.value == nil || sec.value.kind != 's' {
		return
	}
	prov := m.provenance(form, loc)
	reqs := sec.value.list()
	if len(reqs) == 0 {
		rowOf("security:anonymous")
		m.out.security = append(m.out.security, routeshape.Requirement{Schemes: []routeshape.SchemeUse{}, Provenance: prov})
		return
	}
	for _, r := range reqs {
		if r.kind != 'm' {
			continue
		}
		req := routeshape.Requirement{Schemes: []routeshape.SchemeUse{}, Provenance: prov}
		for _, e := range r.fields() {
			if !m.scheme(e.key, loc) {
				continue
			}
			req.Schemes = append(req.Schemes, routeshape.SchemeUse{Name: e.key, Scopes: stringsList(e.value)})
		}
		if len(req.Schemes) == 0 {
			rowOf("security:anonymous")
		}
		m.out.security = append(m.out.security, req)
	}
}

func nonEmpty(v *value) *string {
	s := v.text()
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

// scheme adds the definition of a named scheme (SH-O17); a name the spec
// does not define is a scheme of type unknown.
func (m *shapeMapper) scheme(name string, use location) bool {
	for _, s := range m.out.schemes {
		if s.Name == name {
			return true
		}
	}
	var loc location
	if m.swagger {
		loc = location{m.root, m.root.root.get("securityDefinitions").get(name), "/securityDefinitions/" + escape(name)}
	} else {
		loc = location{m.root, m.root.root.get("components").get("securitySchemes").get(name), "/components/securitySchemes/" + escape(name)}
		if loc.value != nil {
			loc = m.p.resolve(loc)
		}
	}
	if loc.value == nil || loc.value.kind != 'm' {
		m.out.schemes = append(m.out.schemes, routeshape.Scheme{Name: name, Type: routeshape.SchemeUnknown, Provenance: m.provenance("scheme:undefined", use)})
		return true
	}
	v := loc.value
	s := routeshape.Scheme{Name: name, Type: routeshape.SchemeUnknown}
	form := "scheme:other"
	switch t := v.get("type").text(); {
	case t == "http" && !m.swagger:
		form, s.Type = "scheme:http", routeshape.SchemeHTTP
		s.Scheme, s.BearerFormat = nonEmpty(v.get("scheme")), nonEmpty(v.get("bearerFormat"))
	case t == "basic" && m.swagger:
		form, s.Type = "scheme:basic-2.0", routeshape.SchemeHTTP
		basic := "basic"
		s.Scheme = &basic
	case t == "apiKey":
		form, s.Type = "scheme:apiKey", routeshape.SchemeAPIKey
		if in := v.get("in").text(); in == "header" || in == "query" || (in == "cookie" && !m.swagger) {
			s.In = &in
		}
		s.ParamName = nonEmpty(v.get("name"))
	case t == "oauth2":
		form, s.Type = "scheme:oauth2", routeshape.SchemeOAuth2
		s.TokenURL = m.tokenURL(v)
	case t == "openIdConnect" && !m.swagger:
		form, s.Type = "scheme:openIdConnect", routeshape.SchemeOpenIDConnect
		s.OpenIDConnectURL = nonEmpty(v.get("openIdConnectUrl"))
	}
	s.Provenance = m.provenance(form, loc)
	m.out.schemes = append(m.out.schemes, s)
	return true
}

// tokenURL is the token URL of the password or client-credentials flow,
// only when the scheme's such flows name one URL.
func (m *shapeMapper) tokenURL(v *value) *string {
	var urls []string
	if m.swagger {
		if f := v.get("flow").text(); f == "password" || f == "application" {
			if u := nonEmpty(v.get("tokenUrl")); u != nil {
				urls = append(urls, *u)
			}
		}
	} else {
		for _, f := range []string{"password", "clientCredentials"} {
			if u := nonEmpty(v.get("flows").get(f).get("tokenUrl")); u != nil {
				urls = append(urls, *u)
			}
		}
	}
	if len(urls) == 0 {
		return nil
	}
	for _, u := range urls[1:] {
		if u != urls[0] {
			return nil
		}
	}
	return &urls[0]
}

// schema converts a Schema Object (rows SH-O9..SH-O15). depth is the
// nesting level within the item or model, cut at routeshape.MaxSchemaDepth.
func (m *shapeMapper) schema(loc location, depth int) routeshape.Schema {
	v := loc.value
	if depth >= routeshape.MaxSchemaDepth {
		return unknownSchema("schema:depth", fmt.Sprintf("nested deeper than %d levels in the spec", routeshape.MaxSchemaDepth))
	}
	if v == nil {
		return unknownSchema("schema:no-type", "no schema in the spec")
	}
	if v.kind != 'm' {
		if b, ok := v.scalar.(bool); ok && v.kind == 'v' && !m.swagger && m.minor >= 1 {
			return unknownSchema("schema:boolean-schema", fmt.Sprintf("the boolean schema %v is not modelled", b))
		}
		return unknownSchema("schema:no-type", "not a schema object in the spec")
	}
	if ref, ok := v.field("$ref"); ok {
		if !m.swagger && m.minor >= 1 && !onlyAnnotations(v, "$ref") {
			return unknownSchema("ref:siblings", "keywords beside $ref are not modelled")
		}
		return m.ref(loc, ref.value.text())
	}
	for _, k := range []string{"allOf", "oneOf", "anyOf"} {
		if c, ok := v.field(k); ok {
			return m.composition(loc, k, c.value, depth)
		}
	}
	for _, k := range []string{"not", "discriminator"} {
		if _, ok := v.field(k); ok {
			return unknownSchema("schema:composition", k+" is not modelled")
		}
	}
	for _, k := range []string{"if", "then", "else", "dependentSchemas", "dependencies"} {
		if _, ok := v.field(k); ok {
			return unknownSchema("schema:conditional", k+" is not modelled")
		}
	}
	if _, ok := v.field("prefixItems"); ok {
		return unknownSchema("schema:prefixItems", "prefixItems is not modelled")
	}
	if e, ok := v.field("enum"); ok {
		return m.enum("schema:enum", e.value.list(), e.value.kind == 's')
	}
	if c, ok := v.field("const"); ok && !m.swagger && m.minor >= 1 {
		return m.enum("schema:const", []*value{c.value}, true)
	}
	t, ok := v.field("type")
	if !ok {
		return unknownSchema("schema:no-type", "no type in the spec")
	}
	var s routeshape.Schema
	if t.value.kind == 's' {
		if m.swagger || m.minor < 1 {
			return unknownSchema("schema:type-list-3.1", "a list of types is valid from OpenAPI 3.1")
		}
		rowOf("schema:type-list-3.1")
		var others []string
		null := false
		for _, x := range t.value.list() {
			switch n := x.text(); n {
			case "null":
				null = true
			case "":
				return unknownSchema("schema:no-type", "a type that is not a string")
			default:
				if !contains(others, n) {
					others = append(others, n)
				}
			}
		}
		switch len(others) {
		case 0:
			if !null {
				return unknownSchema("schema:no-type", "an empty list of types")
			}
			return m.typed(loc, "null", depth)
		case 1:
			s = m.typed(loc, others[0], depth)
		default:
			return unknownSchema("schema:type-list-3.1", "several types ("+strings.Join(others, ", ")+") are not modelled")
		}
		if null && s.Type != routeshape.TypeUnknown && s.Type != routeshape.TypeNull {
			s.Nullable = true
		}
		return s
	}
	s = m.typed(loc, t.value.text(), depth)
	if !m.swagger && m.minor == 0 && v.get("nullable").boolean() && s.Type != routeshape.TypeUnknown {
		rowOf("schema:nullable-3.0")
		s.Nullable = true
	}
	return s
}

// typed converts a schema of one named type.
func (m *shapeMapper) typed(loc location, name string, depth int) routeshape.Schema {
	v := loc.value
	switch name {
	case "string", "number", "integer", "boolean":
		rowOf("schema:" + name)
		return routeshape.Schema{Type: routeshape.SchemaType(name)}
	case "null":
		if m.swagger || m.minor < 1 {
			return unknownSchema("schema:no-type", "type null is valid from OpenAPI 3.1")
		}
		rowOf("schema:null")
		return routeshape.Schema{Type: routeshape.TypeNull}
	case "file":
		if m.swagger {
			return unknownSchema("swagger:file", "a Swagger file is not modelled")
		}
	case "array":
		rowOf("schema:array")
		items, ok := v.field("items")
		if !ok {
			u := unknownSchema("schema:no-type", "no items schema in the spec")
			return routeshape.Schema{Type: routeshape.TypeArray, Items: &u}
		}
		is := m.schema(location{loc.doc, items.value, loc.pointer + "/items"}, depth+1)
		return routeshape.Schema{Type: routeshape.TypeArray, Items: &is}
	case "object":
		rowOf("schema:object")
		return m.object(loc, depth)
	}
	return unknownSchema("schema:no-type", "type "+strconv.Quote(name)+" is not modelled")
}

// object converts an object schema (SH-O12).
func (m *shapeMapper) object(loc location, depth int) routeshape.Schema {
	v := loc.value
	required := map[string]bool{}
	var requiredOrder []string
	for _, r := range v.get("required").list() {
		if n := r.text(); n != "" && !required[n] {
			required[n] = true
			requiredOrder = append(requiredOrder, n)
		}
	}
	if len(required) > 0 {
		rowOf("schema:required")
	}
	s := routeshape.Schema{Type: routeshape.TypeObject, Additional: routeshape.AdditionalAllowed, Fields: []routeshape.Field{}}
	names := map[string]bool{}
	for _, e := range v.get("properties").fields() {
		if names[e.key] {
			continue
		}
		names[e.key] = true
		ploc := location{loc.doc, e.value, loc.pointer + "/properties/" + escape(e.key)}
		presence := routeshape.Optional
		if required[e.key] {
			presence = routeshape.Required
		}
		// One view serves requests and responses: a field that must not
		// be sent in one direction has no claimed presence in either.
		if marks := m.marks(ploc); marks != 0 {
			rowOf("schema:readOnly-writeOnly")
			presence = routeshape.PresenceUnknown
			if m.model == "" {
				m.marksIn[m.section] |= marks
			} else {
				m.marksOf[m.model] |= marks
			}
		}
		s.Fields = append(s.Fields, routeshape.Field{Name: e.key, Presence: presence, Schema: m.schema(ploc, depth+1)})
	}
	for _, n := range requiredOrder {
		if !names[n] {
			s.Fields = append(s.Fields, routeshape.Field{Name: n, Presence: routeshape.Required, Schema: unknownSchema("schema:no-type", "required without a property schema in the spec")})
		}
	}
	if ap, ok := v.field("additionalProperties"); ok {
		rowOf("schema:additionalProperties")
		if b, isBool := ap.value.scalar.(bool); isBool && !b {
			s.Additional = routeshape.AdditionalForbidden
		}
	}
	for _, k := range []string{"patternProperties", "unevaluatedProperties", "propertyNames"} {
		if _, ok := v.field(k); ok {
			s.Additional = routeshape.AdditionalUnknown
		}
	}
	return s
}

// marks reports the readOnly and writeOnly marks of a property along
// every schema peel() passes through, the path schema() takes to the
// schema that gives the property its type: OpenAPI then applies the
// property only to responses or to requests.
func (m *shapeMapper) marks(loc location) rwMarks {
	var out rwMarks
	chain, _ := m.peel(loc)
	for _, l := range chain {
		if l.value.get("readOnly").boolean() {
			out |= markReadOnly
		}
		if l.value.get("writeOnly").boolean() {
			out |= markWriteOnly
		}
	}
	return out
}

// peel lists the schemas schema() reads through before it reaches the one
// that gives the type: local $ref targets (SH-O13), the member of a single
// allOf (SH-O14) and, from OpenAPI 3.1, both members of a oneOf or anyOf
// of one schema and null (SH-O10). cycle reports a chain that returns to
// a schema already on it: references with no schema in between (SH-O13).
func (m *shapeMapper) peel(loc location) (chain []location, cycle bool) {
	seen := map[*value]bool{}
	for i := 0; i < 64 && loc.value != nil && loc.value.kind == 'm'; i++ {
		v := loc.value
		if seen[v] {
			return chain, true
		}
		seen[v] = true
		chain = append(chain, loc)
		if ref, ok := v.field("$ref"); ok {
			if !m.swagger && m.minor >= 1 && !onlyAnnotations(v, "$ref") {
				return chain, false
			}
			text := ref.value.text()
			if !strings.HasPrefix(text, "#") {
				return chain, false
			}
			u, err := url.Parse(text)
			if err != nil || u.Fragment == "" {
				return chain, false
			}
			target, ok := lookup(loc.doc.root, u.Fragment)
			if !ok {
				return chain, false
			}
			loc = location{loc.doc, target, u.Fragment}
			continue
		}
		keyword, members := "", (*value)(nil)
		for _, k := range []string{"allOf", "oneOf", "anyOf"} {
			if c, ok := v.field(k); ok {
				keyword, members = k, c.value
				break
			}
		}
		if keyword == "" || members == nil || members.kind != 's' || !onlyAnnotations(v, keyword) {
			return chain, false
		}
		list := members.list()
		member := func(i int) location {
			return location{loc.doc, list[i], fmt.Sprintf("%s/%s/%d", loc.pointer, keyword, i)}
		}
		if keyword == "allOf" && len(list) == 1 {
			loc = member(0)
			continue
		}
		if keyword == "allOf" || len(list) != 2 || m.swagger || m.minor < 1 {
			return chain, false
		}
		next := -1
		for i := 0; i < 2; i++ {
			if isNullSchema(list[1-i]) && !isNullSchema(list[i]) {
				next = i
			}
		}
		if next < 0 {
			return chain, false
		}
		chain = append(chain, member(1-next))
		loc = member(next)
	}
	return chain, false
}

// isNullSchema is a 3.1 {type: null} schema beside annotations only.
func isNullSchema(x *value) bool {
	return x != nil && x.kind == 'm' && x.get("type").text() == "null" && onlyAnnotations(x, "type")
}

// acceptsNull reports whether a 3.1 schema accepts null, or whether that
// is not known ("maybe"), read from the spec through peel(): a oneOf of
// such a member and null would reject null (SH-O10).
func (m *shapeMapper) acceptsNull(loc location) (yes, maybe bool) {
	chain, cycle := m.peel(loc)
	if cycle || len(chain) == 0 {
		return false, true
	}
	for _, l := range chain[:len(chain)-1] {
		// A null member peeled on the way: this schema accepts null.
		if isNullSchema(l.value) {
			return true, false
		}
	}
	last := chain[len(chain)-1].value
	if isNullSchema(last) {
		return true, false
	}
	if _, ok := last.field("$ref"); ok {
		return false, true
	}
	for _, k := range []string{"allOf", "oneOf", "anyOf", "not", "if", "then", "else"} {
		if _, ok := last.field(k); ok {
			return false, true
		}
	}
	if e, ok := last.field("enum"); ok {
		for _, x := range e.value.list() {
			if x != nil && x.kind == 'v' && x.scalar == nil && x.scalarText == "" {
				return true, false
			}
		}
		return false, e.value.kind != 's'
	}
	if c, ok := last.field("const"); ok {
		return c.value != nil && c.value.kind == 'v' && c.value.scalar == nil && c.value.scalarText == "", false
	}
	t, ok := last.field("type")
	if !ok {
		return false, true
	}
	if t.value.kind == 's' {
		for _, x := range t.value.list() {
			if x.text() == "null" {
				return true, false
			}
		}
		return false, false
	}
	return t.value.text() == "null", t.value.text() == ""
}

// enum converts enum or const literals (SH-O11).
func (m *shapeMapper) enum(form string, values []*value, list bool) routeshape.Schema {
	if !list || len(values) == 0 {
		return unknownSchema("schema:not-literal", "an enum that is not a list of values")
	}
	rowOf(form)
	out := make([]any, 0, len(values))
	for _, x := range values {
		lit, ok := literal(x)
		if !ok {
			return unknownSchema("schema:not-literal", "an enum value that is not a literal")
		}
		if f, isNumber := lit.(float64); isNumber && !exactNumber(x, f) {
			return unknownSchema("schema:inexact-number", "the enum number "+numberText(x)+" is not stored exactly (beyond float64 precision)")
		}
		out = append(out, lit)
	}
	return routeshape.Schema{Type: routeshape.TypeEnum, Enum: out}
}

// literal reads a scalar as a string, number, boolean or null.
func literal(v *value) (any, bool) {
	if v == nil || v.kind != 'v' {
		return nil, false
	}
	switch s := v.scalar.(type) {
	case string:
		return s, true
	case bool:
		return s, true
	case json.Number:
		f, err := s.Float64()
		return f, err == nil
	case nil:
		if v.scalarText == "" {
			return nil, true
		}
		f, err := strconv.ParseFloat(v.scalarText, 64)
		return f, err == nil
	}
	return nil, false
}

func numberText(v *value) string {
	if n, ok := v.scalar.(json.Number); ok {
		return string(n)
	}
	return v.scalarText
}

// exactNumber reports whether the number f read from a literal is the
// literal's exact value once written back: the shape stores numbers as
// JSON numbers read as float64, so an integer beyond 2^53 or a decimal
// with more digits than float64 holds would be stored as another number.
func exactNumber(v *value, f float64) bool {
	want, ok := new(big.Rat).SetString(numberText(v))
	if !ok {
		return false
	}
	got, ok := new(big.Rat).SetString(strconv.FormatFloat(f, 'g', -1, 64))
	return ok && want.Cmp(got) == 0
}

// composition converts allOf, oneOf and anyOf: a single allOf member is
// that member (SH-O14); a oneOf or anyOf of one schema and null is that
// schema, nullable (SH-O10, OpenAPI 3.1); anything else is unknown.
func (m *shapeMapper) composition(loc location, keyword string, members *value, depth int) routeshape.Schema {
	list := members.list()
	if members == nil || members.kind != 's' {
		return unknownSchema("schema:composition", keyword+" is not a list")
	}
	if !onlyAnnotations(loc.value, keyword) {
		return unknownSchema("schema:composition", keyword+" beside other keywords is not modelled")
	}
	member := func(i int) location {
		return location{loc.doc, list[i], fmt.Sprintf("%s/%s/%d", loc.pointer, keyword, i)}
	}
	if keyword == "allOf" && len(list) == 1 {
		rowOf("schema:allOf-one")
		return m.schema(member(0), depth)
	}
	if keyword != "allOf" && len(list) == 2 && !m.swagger && m.minor >= 1 {
		for i := 0; i < 2; i++ {
			if isNullSchema(list[1-i]) && !isNullSchema(list[i]) {
				s := m.schema(member(i), depth)
				if s.Type == routeshape.TypeUnknown {
					return s
				}
				// oneOf rejects a value that matches both members: with a
				// member that accepts null itself, null is not accepted.
				if yes, maybe := m.acceptsNull(member(i)); keyword == "oneOf" && (yes || maybe) {
					return unknownSchema("schema:composition", "oneOf of null and a schema that may accept null is not modelled")
				}
				rowOf("schema:null-member")
				s.Nullable = true
				return s
			}
		}
	}
	return unknownSchema("schema:composition", keyword+" is not modelled")
}

// ref converts a $ref (SH-O13): a reference within the same document is
// a model named by its file and JSON pointer, built once, so cycles end
// at the model; any other reference is unknown.
func (m *shapeMapper) ref(loc location, text string) routeshape.Schema {
	if !strings.HasPrefix(text, "#") {
		return unknownSchema("ref:external", "reference "+strconv.Quote(text)+" to another document is not resolved")
	}
	u, err := url.Parse(text)
	if err != nil {
		return unknownSchema("ref:missing", "reference "+strconv.Quote(text)+" is not a valid URI")
	}
	pointer := u.Fragment
	name := loc.doc.file + "#" + pointer
	if _, ok := m.models[name]; ok {
		rowOf("ref:local")
		m.noteRef(name)
		return routeshape.Schema{Type: routeshape.TypeRef, Ref: name}
	}
	target, ok := lookup(loc.doc.root, pointer)
	if !ok || pointer == "" {
		return unknownSchema("ref:missing", "reference "+strconv.Quote(text)+" names nothing in the spec")
	}
	tloc := location{loc.doc, target, pointer}
	if _, cycle := m.peel(tloc); cycle {
		m.p.shapeDiag(tloc, "openapi_ref_unresolved", fmt.Sprintf("reference %q: reference cycle with no schema in between", text))
		return unknownSchema("ref:cycle", "reference cycle")
	}
	m.noteRef(name)
	m.models[name] = -1
	outer := m.model
	m.model = name
	schema := m.schema(tloc, 0)
	m.model = outer
	m.models[name] = len(m.out.models)
	m.out.models = append(m.out.models, routeshape.Model{Name: name, Schema: schema, Provenance: m.provenance("ref:local", tloc)})
	return routeshape.Schema{Type: routeshape.TypeRef, Ref: name}
}

// noteRef records that the schema being read references model name, for
// marksNote.
func (m *shapeMapper) noteRef(name string) {
	if m.model == "" {
		m.refsIn[m.section] = append(m.refsIn[m.section], name)
	} else {
		m.refsOf[m.model] = append(m.refsOf[m.model], name)
	}
}

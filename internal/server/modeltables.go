package server

import (
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/query"
)

// This file links route shapes to database models (T029, rows
// MT-20..MT-29 of docs/contract/claims/route-model-types.md): the schema
// view's table detail lists the routes that accept or return the table's
// model, and the route detail notes which of its shape's models are also
// tables. A link needs proven identity: the shape's model and the table
// are the same declaration node. Two classes with the same fields, or the
// same name in another file, are never linked.

// Rows of the claim contract, carried by the entries they produce.
const (
	rowModelIdentity    = "MT-20"
	rowUseBody          = "MT-21"
	rowUseParameter     = "MT-22"
	rowUseResponse      = "MT-23"
	rowUseNested        = "MT-24"
	rowRoutesListed     = "MT-25"
	rowRoutesAbsence    = "MT-26"
	rowNoDeclaration    = "MT-27"
	rowRouteModelTables = "MT-28"
	rowModelNotLinked   = "MT-29"
)

// modelTableRow is one row of the contract section; modelTableRows and
// the document are kept 1:1 by TestModelTableContractTable, and each row
// names the test that checks it.
type modelTableRow struct {
	ID    string
	Topic string
	Test  string
}

// modelTableRows is the contract section in table order.
var modelTableRows = []modelTableRow{
	{rowModelIdentity, "same declaration", "TestModelTableIdentity"},
	{rowUseBody, "request body", "TestModelRoutesFixture"},
	{rowUseParameter, "request parameter", "TestModelUsesSynthetic"},
	{rowUseResponse, "response", "TestModelRoutesFixture"},
	{rowUseNested, "nested in another model", "TestModelRoutesFixture"},
	{rowRoutesListed, "table detail list", "TestModelRoutesFixture"},
	{rowRoutesAbsence, "absence", "TestModelRoutesAbsence"},
	{rowNoDeclaration, "no declaration", "TestModelRoutesNoDeclaration"},
	{rowRouteModelTables, "route detail", "TestRouteModelTablesFixture"},
	{rowModelNotLinked, "everything else", "TestModelTableIdentity"},
}

// Model route states (row MT-25..MT-27).
const (
	// modelRoutesListed: at least one route's shape uses the model.
	modelRoutesListed = "listed"
	// modelRoutesNone: every route stores a shape that is fully read and
	// none uses the model: a proven absence.
	modelRoutesNone = "none"
	// modelRoutesNoneFound: no route whose shape was read uses the model,
	// but some routes have parts not read, so a use there is not ruled
	// out.
	modelRoutesNoneFound = "none_found"
	// modelRoutesNotRead: no route of the repository stores a shape, so
	// nothing is known either way.
	modelRoutesNotRead = "not_read"
	// modelRoutesNoDeclaration: the table names no stored class node, so
	// no route model can be proven to be its declaration.
	modelRoutesNoDeclaration = "no_declaration"
)

// modelRoutesDoc is the model_routes field of GET /api/v0/schema/table for
// a model table.
type modelRoutesDoc struct {
	State string `json:"state"`
	Row   string `json:"row"`
	// Class is the class node the table is declared by, or null.
	Class  *string           `json:"class"`
	Routes []modelRouteEntry `json:"routes"`
	// RoutesTotal counts the stored routes, RoutesWithShape those with a
	// valid stored shape and RoutesNotFullyRead those whose request or
	// response may hold a use Graphvein did not read (no shape, a section
	// not read or partial, an unread or unknown schema).
	RoutesTotal        int `json:"routes_total"`
	RoutesWithShape    int `json:"routes_with_shape"`
	RoutesNotFullyRead int `json:"routes_not_fully_read"`
	// Links maps each "file:line" a use's provenance chip shows to its
	// editor link (as shape_links); absent when there is none.
	Links map[string]*query.IDELink `json:"links,omitempty"`
}

// modelRouteEntry is one route whose shape uses the model.
type modelRouteEntry struct {
	Route     string  `json:"route"`
	Method    *string `json:"method"`
	Path      *string `json:"path"`
	Pattern   *string `json:"pattern"`
	Framework *string `json:"framework"`
	Scope     any     `json:"scope"`
	// Model is the name the route's shape gives the model.
	Model string     `json:"model"`
	Uses  []modelUse `json:"uses"`
}

// modelUse is one place of a route's shape that refers to the model.
type modelUse struct {
	// Direction is request (body or a parameter) or response.
	Direction string `json:"direction"`
	// Section is the shape section: body, responses, path, query, header
	// or cookie.
	Section string `json:"section"`
	// Name is the parameter's name (parameters only).
	Name *string `json:"name"`
	// Status is the response status (responses only).
	Status      *string `json:"status"`
	ContentType *string `json:"content_type"`
	// Via lists the models the reference was reached through, outermost
	// first; empty for a direct use (the item's schema, through arrays).
	Via []string `json:"via"`
	// Nested is true when the reference is inside an object (a field),
	// not the item's value itself.
	Nested bool `json:"nested"`
	// Provenance is the shape item's: how Graphvein knows the route
	// accepts or returns this value.
	Provenance routeshape.Provenance `json:"provenance"`
	Row        string                `json:"row"`
}

// routeModelTable is one entry of the route detail's model_tables: a
// model of the route's shape that is also a database table.
type routeModelTable struct {
	Model string  `json:"model"`
	Class string  `json:"class"`
	Table string  `json:"table"`
	Name  *string `json:"name"`
	Set   *string `json:"set"`
	Tool  *string `json:"tool"`
	Row   string  `json:"row"`
}

// tableClass returns the class node identity a model table names in its
// class attribute (SQLModel writes the class node's identity); "" when
// the attribute is not a class node identity.
func tableClass(table contract.Node) string {
	s, ok := table.Attributes["class"].(string)
	if !ok {
		return ""
	}
	kind, _, _, err := contract.ParseIdentity(s)
	if err != nil || kind != "class" {
		return ""
	}
	return s
}

// modelIsClass reports whether a shape model is the class node: the
// model's provenance is the class statement (same file, same start line)
// and the model has the class's name, or the file-qualified form the
// FastAPI shape extractor gives a name two classes share (Name__...).
// A model mapped from a committed spec is never a code declaration (row
// MT-20; everything else MT-29).
func modelIsClass(m routeshape.Model, class contract.Node) bool {
	if class.Kind != "class" || class.Span == nil || class.Name == "" {
		return false
	}
	switch m.Provenance.Kind {
	case routeshape.Fact, routeshape.Intended, routeshape.Returned:
	default:
		return false
	}
	if m.Provenance.File != class.File || m.Provenance.Line != class.Span.StartLine {
		return false
	}
	return m.Name == class.Name || strings.HasPrefix(m.Name, class.Name+"__")
}

// shapeModelsOf returns the names of the shape's models that are the
// class node.
func shapeModelsOf(s *routeshape.Shape, class contract.Node) []string {
	var out []string
	for _, m := range s.Models {
		if modelIsClass(m, class) {
			out = append(out, m.Name)
		}
	}
	return out
}

// modelUses lists the places of a shape that refer to the model named
// target: parameter, body and response items whose schema refers to it,
// directly (through arrays) or nested in fields and other models (rows
// MT-21..MT-24). Security is not a request or response value.
func modelUses(s *routeshape.Shape, target string) []modelUse {
	models := map[string]*routeshape.Model{}
	for i := range s.Models {
		models[s.Models[i].Name] = &s.Models[i]
	}
	var out []modelUse
	add := func(base modelUse, schema *routeshape.Schema) {
		if schema == nil {
			return
		}
		for _, r := range schemaRefs(schema, target, models) {
			u := base
			u.Via, u.Nested = r.via, r.nested
			if r.nested {
				u.Row = rowUseNested
			}
			out = append(out, u)
		}
	}
	params := []struct {
		name string
		sec  *routeshape.ParamSection
	}{
		{routeshape.SectionPath, &s.Path}, {routeshape.SectionQuery, &s.Query},
		{routeshape.SectionHeader, &s.Header}, {routeshape.SectionCookie, &s.Cookie},
	}
	for _, p := range params {
		for _, it := range p.sec.Items {
			name := it.Name
			add(modelUse{Direction: "request", Section: p.name, Name: &name, Provenance: it.Provenance, Row: rowUseParameter}, it.Schema)
		}
	}
	for _, it := range s.Body.Items {
		add(modelUse{Direction: "request", Section: routeshape.SectionBody, ContentType: it.ContentType, Provenance: it.Provenance, Row: rowUseBody}, it.Schema)
	}
	for _, it := range s.Responses.Items {
		status := it.Status
		add(modelUse{Direction: "response", Section: routeshape.SectionResponses, Status: &status, ContentType: it.ContentType, Provenance: it.Provenance, Row: rowUseResponse}, it.Schema)
	}
	return out
}

// schemaRef is one reference to the target model found in a schema.
type schemaRef struct {
	via    []string
	nested bool
}

// schemaRefs finds the references to target in a schema. Arrays keep a
// reference direct; object fields and other models make it nested. Each
// model is entered once per path, so a cycle ends. A schema reaches the
// target at most once per distinct (via, nested) path.
func schemaRefs(schema *routeshape.Schema, target string, models map[string]*routeshape.Model) []schemaRef {
	var out []schemaRef
	seen := map[string]bool{}
	var walk func(s *routeshape.Schema, via []string, nested bool, depth int)
	walk = func(s *routeshape.Schema, via []string, nested bool, depth int) {
		if s == nil || depth > routeshape.MaxSchemaDepth {
			return
		}
		switch s.Type {
		case routeshape.TypeRef:
			if s.Ref == target {
				key := strings.Join(via, "\x00")
				if nested {
					key += "\x01"
				}
				if !seen[key] {
					seen[key] = true
					out = append(out, schemaRef{via: append([]string{}, via...), nested: nested})
				}
				return
			}
			m, ok := models[s.Ref]
			if !ok || containsString(via, s.Ref) {
				return
			}
			walk(&m.Schema, append(append([]string{}, via...), s.Ref), nested, depth+1)
		case routeshape.TypeArray:
			walk(s.Items, via, nested, depth+1)
		case routeshape.TypeObject:
			for i := range s.Fields {
				walk(&s.Fields[i].Schema, via, true, depth+1)
			}
		}
	}
	walk(schema, []string{}, false, 0)
	return out
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// shapeFullyRead reports whether a shape leaves no place where a request
// or response value could use a model Graphvein did not see (row MT-26):
// the parameter, body and response sections are each complete or
// provably none as the code proves them, and every schema they hold is
// read to its end (no unknown or unread schema, no object accepting
// fields it does not list, no reference to a model that is not in the
// shape or is mapped from a spec).
func shapeFullyRead(s *routeshape.Shape) bool {
	for _, sec := range []routeshape.Section{s.Path.Section, s.Query.Section, s.Header.Section, s.Cookie.Section, s.Body.Section, s.Responses.Section} {
		switch sec.CodeState() {
		case routeshape.Complete, routeshape.ProvablyNone:
		default:
			return false
		}
	}
	models := map[string]*routeshape.Model{}
	for i := range s.Models {
		models[s.Models[i].Name] = &s.Models[i]
	}
	read := func(schema *routeshape.Schema) bool {
		return schema != nil && schemaFullyRead(schema, models, map[string]bool{}, 0)
	}
	for _, p := range []routeshape.ParamSection{s.Path, s.Query, s.Header, s.Cookie} {
		for _, it := range p.Items {
			if !read(it.Schema) {
				return false
			}
		}
	}
	for _, it := range s.Body.Items {
		if !read(it.Schema) {
			return false
		}
	}
	for _, it := range s.Responses.Items {
		if !read(it.Schema) {
			return false
		}
	}
	return true
}

func schemaFullyRead(s *routeshape.Schema, models map[string]*routeshape.Model, entered map[string]bool, depth int) bool {
	if depth > routeshape.MaxSchemaDepth {
		return false
	}
	switch s.Type {
	case routeshape.TypeUnknown:
		return false
	case routeshape.TypeRef:
		m, ok := models[s.Ref]
		if !ok || m.Provenance.Kind == routeshape.Mapped {
			return false
		}
		if entered[s.Ref] {
			return true
		}
		entered[s.Ref] = true
		return schemaFullyRead(&m.Schema, models, entered, depth+1)
	case routeshape.TypeArray:
		return s.Items != nil && schemaFullyRead(s.Items, models, entered, depth+1)
	case routeshape.TypeObject:
		// An object that accepts fields it does not list (a map such as
		// dict[str, Hero], or extra fields) may hold values whose schema
		// was not read.
		if s.Additional != routeshape.AdditionalForbidden {
			return false
		}
		for i := range s.Fields {
			if !schemaFullyRead(&s.Fields[i].Schema, models, entered, depth+1) {
				return false
			}
		}
	}
	return true
}

// modelRoutesOf builds a model table's model_routes from the stored
// routes (rows MT-25..MT-27). class is the table's class node, nil when
// the table names none or it is not stored.
func modelRoutesOf(p *presenter, class *contract.Node, routes []contract.Node) modelRoutesDoc {
	out := modelRoutesDoc{Routes: []modelRouteEntry{}, RoutesTotal: len(routes)}
	if class == nil {
		out.State, out.Row = modelRoutesNoDeclaration, rowNoDeclaration
		return out
	}
	id := class.Identity
	out.Class = &id
	var positions []shapePosition
	for _, r := range routes {
		s, _ := routeShape(r)
		if s == nil {
			out.RoutesNotFullyRead++
			continue
		}
		out.RoutesWithShape++
		if !shapeFullyRead(s) {
			out.RoutesNotFullyRead++
		}
		for _, name := range shapeModelsOf(s, *class) {
			uses := modelUses(s, name)
			if len(uses) == 0 {
				continue
			}
			for _, u := range uses {
				positions = append(positions, shapePosition{file: u.Provenance.File, line: u.Provenance.Line})
			}
			out.Routes = append(out.Routes, modelRouteEntry{
				Route: r.Identity, Method: stringAttr(r, "method"), Path: routePath(r, scopeProduction),
				Pattern: stringAttr(r, "pattern"), Framework: stringAttr(r, "framework"), Scope: r.Attributes["scope"],
				Model: name, Uses: uses,
			})
		}
	}
	sort.SliceStable(out.Routes, func(i, j int) bool {
		a, b := out.Routes[i], out.Routes[j]
		if pa, pb := derefOr(a.Path, a.Pattern), derefOr(b.Path, b.Pattern); pa != pb {
			return pa < pb
		}
		if ma, mb := derefOr(a.Method, nil), derefOr(b.Method, nil); ma != mb {
			return ma < mb
		}
		if a.Route != b.Route {
			return a.Route < b.Route
		}
		return a.Model < b.Model
	})
	switch {
	case len(out.Routes) > 0:
		out.State, out.Row = modelRoutesListed, rowRoutesListed
	case out.RoutesWithShape == 0:
		out.State, out.Row = modelRoutesNotRead, rowRoutesAbsence
	case out.RoutesNotFullyRead == 0:
		out.State, out.Row = modelRoutesNone, rowRoutesAbsence
	default:
		out.State, out.Row = modelRoutesNoneFound, rowRoutesAbsence
	}
	for _, pos := range positions {
		if p == nil || pos.file == "" || pos.line < 1 {
			continue
		}
		link := p.link(pos.file, contract.Span{StartLine: pos.line, EndLine: pos.line})
		if link == nil {
			continue
		}
		if out.Links == nil {
			out.Links = map[string]*query.IDELink{}
		}
		out.Links[shapeLinkKey(pos.file, pos.line)] = link
	}
	return out
}

func derefOr(s, fallback *string) string {
	if s != nil {
		return *s
	}
	if fallback != nil {
		return *fallback
	}
	return ""
}

// tableModelRoutes reads what modelRoutesOf needs for one table: its
// class node and every stored route. It returns nil for a table that is
// not a model table (a migration table has no declaration a route could
// use, so the field is left out).
func tableModelRoutes(rd *reader, p *presenter, table contract.Node) (*modelRoutesDoc, error) {
	if !modelRecord(table) {
		return nil, nil
	}
	routes, err := rd.nodes("route")
	if err != nil {
		return nil, err
	}
	var class *contract.Node
	if id := tableClass(table); id != "" {
		found, err := rd.byIdentity([]string{id})
		if err != nil {
			return nil, err
		}
		if n, ok := found[id]; ok && n.Kind == "class" {
			class = &n
		}
	}
	doc := modelRoutesOf(p, class, routes)
	return &doc, nil
}

// routeModelTables lists the models of a route's shape that are also
// database tables (row MT-28), by model name then table identity. It
// returns nil without a shape (the field is then absent) and an empty
// list when no model is a table.
func routeModelTables(rd *reader, shape *routeshape.Shape) (*[]routeModelTable, error) {
	if shape == nil {
		return nil, nil
	}
	out := []routeModelTable{}
	if len(shape.Models) == 0 {
		return &out, nil
	}
	tables, err := rd.nodes("table")
	if err != nil {
		return nil, err
	}
	byClass := map[string][]contract.Node{}
	var classIDs []string
	for _, t := range tables {
		if !modelRecord(t) {
			continue
		}
		if id := tableClass(t); id != "" {
			if _, seen := byClass[id]; !seen {
				classIDs = append(classIDs, id)
			}
			byClass[id] = append(byClass[id], t)
		}
	}
	if len(classIDs) == 0 {
		return &out, nil
	}
	classes, err := rd.byIdentity(classIDs)
	if err != nil {
		return nil, err
	}
	sort.Strings(classIDs)
	for _, m := range shape.Models {
		for _, id := range classIDs {
			c, ok := classes[id]
			if !ok || !modelIsClass(m, c) {
				continue
			}
			for _, t := range byClass[id] {
				out = append(out, routeModelTable{
					Model: m.Name, Class: id, Table: t.Identity,
					Name: stringValue(t.Attributes["name"]), Set: stringValue(t.Attributes["set"]), Tool: stringValue(t.Attributes["tool"]),
					Row: rowRouteModelTables,
				})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Model != out[j].Model {
			return out[i].Model < out[j].Model
		}
		return out[i].Table < out[j].Table
	})
	return &out, nil
}

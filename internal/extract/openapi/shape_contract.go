package openapi

// specShapeRow is one row of the committed spec shape claim contract
// (docs/contract/claims/route-shape-spec.md). Forms are the spec forms
// the row reads, written "group:form"; TestSpecShapeContractTable keeps
// the table and the document 1:1.
type specShapeRow struct {
	ID    string
	Title string
	Forms []string
}

// specShapeRows is the claim contract in table order. A spec form no row
// lists goes to D: it is not stored.
var specShapeRows = []specShapeRow{
	{"SH-O1", "linked route", []string{"link:documents_route"}},
	{"SH-O2", "several documenting operations", []string{"link:several"}},
	{"SH-O3", "section states", []string{"state:spec_only", "state:code_kept", "state:provably_none_kept"}},
	{"SH-O4", "parameters", []string{"param:path", "param:query", "param:header", "param:cookie", "param:override", "param:content"}},
	{"SH-O5", "ignored header parameters", []string{"param:reserved-header"}},
	{"SH-O6", "Swagger 2.0 parameters", []string{"swagger:parameter", "swagger:body", "swagger:formData", "swagger:file"}},
	{"SH-O7", "request body", []string{"body:content", "body:required", "body:no-schema"}},
	{"SH-O8", "responses", []string{"response:status", "response:range", "response:default", "response:content", "response:no-content", "response:swagger", "response:swagger-range"}},
	{"SH-O9", "schema types", []string{"schema:string", "schema:number", "schema:integer", "schema:boolean", "schema:null", "schema:object", "schema:array", "schema:no-type"}},
	{"SH-O10", "null", []string{"schema:nullable-3.0", "schema:type-list-3.1", "schema:null-member"}},
	{"SH-O11", "enum and const", []string{"schema:enum", "schema:const", "schema:inexact-number"}},
	{"SH-O12", "object fields", []string{"schema:required", "schema:additionalProperties", "schema:readOnly-writeOnly"}},
	{"SH-O13", "references", []string{"ref:local", "ref:external", "ref:missing", "ref:siblings", "ref:cycle"}},
	{"SH-O14", "single allOf member", []string{"schema:allOf-one"}},
	{"SH-O15", "schemas not modelled", []string{"schema:composition", "schema:conditional", "schema:prefixItems", "schema:boolean-schema", "schema:depth", "schema:not-literal"}},
	{"SH-O16", "security requirements", []string{"security:operation", "security:root", "security:anonymous"}},
	{"SH-O17", "security schemes", []string{"scheme:http", "scheme:apiKey", "scheme:oauth2", "scheme:openIdConnect", "scheme:basic-2.0", "scheme:other", "scheme:undefined", "scheme:renamed"}},
	{"SH-O18", "spec differs", []string{"differs:presence", "differs:type", "differs:names", "differs:body", "differs:status", "differs:security"}},
	{"SH-O19", "generated document", nil},
	{"D", "anything no row covers", nil},
}

// specShapeOwner maps each form to its row, built once from the table so
// an item's rule is always the row that lists its form.
var specShapeOwner = func() map[string]string {
	out := map[string]string{}
	for _, r := range specShapeRows {
		for _, f := range r.Forms {
			out[f] = r.ID
		}
	}
	return out
}()

// specShapeTag records the forms a test run reaches (nil outside tests).
var specShapeTag func(form string)

// TraceSpecShapeForms makes every later mapped shape read report the
// forms of docs/contract/claims/route-shape-spec.md it reaches, so a
// fixture test outside this package can check that its golden exercises
// every row. It returns the function that stops tracing. Tests only: it
// is not safe for concurrent indexing runs.
func TraceSpecShapeForms(f func(form string)) (stop func()) {
	specShapeTag = f
	return func() { specShapeTag = nil }
}

// SpecShapeForms lists every form of the claim contract in table order.
func SpecShapeForms() []string {
	var out []string
	for _, r := range specShapeRows {
		out = append(out, r.Forms...)
	}
	return out
}

// rowOf returns the row that lists form and records the form for the
// fixture coverage test. An unlisted form panics in tests only through
// the contract test, never at run time: it returns "D".
func rowOf(form string) string {
	if specShapeTag != nil {
		specShapeTag(form)
	}
	if row, ok := specShapeOwner[form]; ok {
		return row
	}
	return "D"
}

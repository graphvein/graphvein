package routes

// axumShapeRow is one row of the Axum route shape claim contract
// (docs/contract/claims/route-shape-axum.md). Forms are the code forms the
// row reads; TestAxumShapeContractTable keeps this table and the document
// 1:1.
type axumShapeRow struct {
	ID    string
	Title string
	Forms []string
}

// Rule names the rows items carry as provenance.
const (
	axumRowTemplate     = "SH-X1"
	axumRowPathTyped    = "SH-X2"
	axumRowPathStruct   = "SH-X3"
	axumRowQuery        = "SH-X4"
	axumRowQueryRead    = "SH-X5"
	axumRowJSON         = "SH-X6"
	axumRowForm         = "SH-X7"
	axumRowRawBody      = "SH-X8"
	axumRowTypedHeader  = "SH-X10"
	axumRowHeaderRead   = "SH-X11"
	axumRowCustom       = "SH-X13"
	axumRowJSONResponse = "SH-X21"
	axumRowStatusTuple  = "SH-X22"
	axumRowStatus       = "SH-X23"
	axumRowRedirect     = "SH-X24"
	axumRowText         = "SH-X25"
	axumRowError        = "SH-X26"
	axumRowStates       = "SH-X29"
)

// axumShapeRows is the claim contract in table order. A form no row lists
// goes to the default row D: it claims nothing and the section it would
// fill says so in its reason.
var axumShapeRows = []axumShapeRow{
	{"SH-X1", "path template parameters", []string{"template:{name}", "template:{*name}", "template:none"}},
	{"SH-X2", "Path scalar or tuple", []string{"extractor:Path<scalar>", "extractor:Path<tuple>"}},
	{"SH-X3", "Path struct", []string{"extractor:Path<struct>"}},
	{"SH-X4", "Query struct", []string{"extractor:Query<struct>", "unknown:url-encoded-type"}},
	{"SH-X5", "Query map reads", []string{"extractor:Query<HashMap>", "read:map.get"}},
	{"SH-X6", "Json body", []string{"extractor:Json<T>"}},
	{"SH-X7", "Form", []string{"extractor:Form<T>", "method:ANY", "method:HEAD"}},
	{"SH-X8", "raw body", []string{"extractor:String", "extractor:Bytes"}},
	{"SH-X9", "one body extractor", []string{"body:several"}},
	{"SH-X10", "typed headers", []string{"extractor:TypedHeader<T>", "extractor:Option<TypedHeader<T>>"}},
	{"SH-X11", "HeaderMap reads", []string{"read:headers.get", "read:headers.get_all", "read:headers.contains_key", "name:literal", "name:header-constant", "name:const", "version:HeaderMap-takes"}},
	{"SH-X12", "extractors without request parameters", []string{"extractor:State", "extractor:Extension", "extractor:Method", "extractor:MatchedPath", "extractor:ConnectInfo", "extractor:NestedPath"}},
	{"SH-X13", "repository extractors", []string{"impl:FromRequestParts", "impl:FromRequest"}},
	{"SH-X14", "raw request values", []string{"extractor:Request", "extractor:Parts", "extractor:RequestParts", "extractor:Uri", "extractor:OriginalUri", "extractor:RawQuery", "read:dynamic", "read:any-use", "chain:stopped"}},
	{"SH-X15", "wrapped and unknown extractors", []string{"extractor:Option<X>", "extractor:Result<X,E>", "extractor:unknown"}},
	{"SH-X16", "serde structs", []string{"serde:derive", "serde:rename", "serde:rename_all", "serde:alias", "serde:default", "serde:skip", "serde:skip_serializing", "serde:skip_deserializing", "serde:skip_serializing_if", "serde:deny_unknown_fields", "type:Option", "type:Box<Option>"}},
	{"SH-X17", "serde unit enums", []string{"serde:enum-unit"}},
	{"SH-X18", "refused serde forms", []string{"unknown:no-derive", "unknown:with", "unknown:flatten", "unknown:tagged-enum", "unknown:data-enum", "unknown:container-attribute", "unknown:recursive", "unknown:tuple", "unknown:attribute"}},
	{"SH-X19", "scalar and library types", []string{"type:scalar", "type:sequence", "type:map", "type:Uuid", "type:chrono", "type:serde_json::Value", "type:external"}},
	{"SH-X20", "generics and aliases", []string{"type:generic-instance", "type:alias", "unknown:generic"}},
	{"SH-X21", "Json response", []string{"response:Json<T>", "response:Json(value)", "unknown:rebound-name"}},
	{"SH-X22", "status tuples", []string{"response:(StatusCode,T)", "parts:content-type"}},
	{"SH-X23", "status codes", []string{"response:StatusCode", "status:no-body"}},
	{"SH-X24", "redirects", []string{"response:Redirect::temporary", "response:Redirect::permanent", "unknown:Redirect::to"}},
	{"SH-X25", "text and empty responses", []string{"response:String", "response:&str", "response:Html", "response:()", "response:literal", "version:text"}},
	{"SH-X26", "Result and error variants", []string{"response:Result<T,E>", "response:Err(E::V)", "unknown:?", "arm:self-match", "arm:guard", "unknown:never-ok"}},
	{"SH-X27", "impl IntoResponse", []string{"response:impl IntoResponse", "response:Response", "response:into_response", "merge:different-schemas"}},
	{"SH-X28", "call following", []string{"call:function", "call:Self::f", "call:self.f", "call:extractor-impl", "unknown:local-binding"}},
	{"SH-X29", "section states and chains", []string{"state:path", "state:query", "state:header", "state:cookie", "state:body", "state:responses", "state:security"}},
	{"D", "anything no row covers", nil},
}

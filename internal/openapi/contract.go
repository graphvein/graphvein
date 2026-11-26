package openapi

import "github.com/graphvein/graphvein/internal/extract/routeshape"

// Refusal reasons of x-graphvein.unrepresentable (rows OG-11..OG-14,
// OG-20).
const (
	ReasonInvalidTemplate = "invalid_template"
	ReasonDuplicateParam  = "duplicate_parameter_name"
	ReasonWildcard        = "wildcard"
	ReasonOptional        = "optional_parameter"
	ReasonHost            = "host_pattern"
	ReasonMethod          = "method_not_in_openapi_3_1"
	ReasonResolution      = "unknown_resolution"
)

// Conflict reasons of x-graphvein.conflicts (rows OG-15, OG-20, OG-31).
const (
	ConflictEquivalent = "equivalent_templates"
	ConflictDuplicate  = "duplicate_operation"
	ConflictScheme     = "scheme_name_conflict"
)

// Reasons and unknown parts of x-graphvein.unresolved (row OG-45).
const (
	UnresolvedNoPath = "no_path"
	UnresolvedBase   = "unresolved_base"
	PartBasePath     = "base_path"
	PartMount        = "mount_or_prefix"
)

// notReadFields is OG-41's list in its fixed order.
var notReadFields = []string{"query", "header", "cookie", "requestBody", "responses", "operationId", "summary", "tags", "security", "path_parameter_types"}

// Row is one row of the claim contract
// (docs/contract/claims/openapi-generation.md). Forms are the modelled
// values the row covers, written "group:value"; every value the generator
// can write in a listed group is the form of exactly one row
// (TestContractTable checks it).
type Row struct {
	ID    string
	Title string
	Forms []string
}

func forms(group string, values ...string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = group + ":" + v
	}
	return out
}

func join(lists ...[]string) []string {
	var out []string
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}

// Rows is the claim contract in table order. A field no row lists is
// never written (row D).
var Rows = []Row{
	{"OG-1", "openapi version", forms("openapi", Version)},
	{"OG-2", "info.title", nil},
	{"OG-3", "info.version", forms("info.version", "unversioned")},
	{"OG-4", "info.description", nil},
	{"OG-5", "servers", nil},
	{"OG-6", "paths", nil},
	{"OG-7", "components", nil},
	{"OG-8", "root tags", nil},
	{"OG-9", "root x-graphvein", nil},
	{"OG-10", "path key", forms("refused", ReasonResolution)},
	{"OG-11", "path key parameters", forms("refused", ReasonInvalidTemplate, ReasonDuplicateParam)},
	{"OG-12", "multi-segment wildcards", forms("refused", ReasonWildcard)},
	{"OG-13", "optional parameters", forms("refused", ReasonOptional)},
	{"OG-14", "host-restricted patterns", forms("refused", ReasonHost)},
	{"OG-15", "equivalent templates", forms("conflict", ConflictEquivalent)},
	{"OG-16", "path item x-graphvein", nil},
	{"OG-17", "ANY routes", forms("method", "ANY")},
	{"OG-18", "Starlette path convertor", forms("convertor", "path")},
	{"OG-19", "first-match shadowing", forms("first_match", "express", "fastapi")},
	{"OG-20", "operations", join(forms("method", methodOrder...), forms("refused", ReasonMethod), forms("conflict", ConflictDuplicate))},
	{"OG-21", "implicit methods", nil},
	{"OG-22", "operationId", nil},
	{"OG-23", "summary and description", nil},
	{"OG-24", "operation tags", nil},
	{"OG-25", "deprecated", nil},
	{"OG-26", "path parameters", nil},
	{"OG-27", "query, header and cookie parameters without a shape", nil},
	{"OG-28", "requestBody without a shape", nil},
	{"OG-29", "responses without a shape", nil},
	{"OG-30", "security from FastAPI dependencies", nil},
	{"OG-31", "security schemes from FastAPI classes", append(forms("class", "HTTPBearer", "HTTPBasic", "HTTPDigest"), "conflict:"+ConflictScheme)},
	{"OG-32", "operation x-graphvein", nil},
	{"OG-40", "x-graphvein.rule", nil},
	{"OG-41", "x-graphvein.not_read", forms("not_read", notReadFields...)},
	{"OG-42", "x-graphvein.auth", forms("auth", authKeys...)},
	{"OG-43", "resolution, conditional and assumptions", forms("resolution", "literal", "constant", "settings_default")},
	{"OG-44", "x-graphvein.documented_by", nil},
	{"OG-45", "x-graphvein.unresolved", join(forms("unresolved", UnresolvedNoPath, UnresolvedBase), forms("unknown", PartBasePath, PartMount), forms("resolution", "unresolved"))},
	{"OG-46", "x-graphvein.unrepresentable", nil},
	{"OG-47", "x-graphvein.conflicts", nil},
	{"OG-48", "x-graphvein.excluded_test", nil},
	{"OG-49", "x-graphvein.counts", nil},
	{"OG-50", "document grouping", nil},
	{"OG-51", "ordering", nil},
	{"OG-52", "serialisation", forms("format", FormatJSON, FormatYAML)},
	{"OG-60", "imported specs", nil},
	{"OG-61", "project mode", nil},
	{"OG-70", "shape attribute", nil},
	{"OG-71", "section states", forms("state", string(routeshape.NotRead), string(routeshape.ProvablyNone), string(routeshape.Partial), string(routeshape.Complete))},
	{"OG-72", "path parameter types from facts", forms("section", routeshape.SectionPath)},
	{"OG-73", "fact path parameter not in the key", nil},
	{"OG-74", "fact query, header and cookie parameters", join(forms("kind", string(routeshape.Fact)), forms("access", string(routeshape.Declared)),
		forms("section", routeshape.SectionQuery, routeshape.SectionHeader, routeshape.SectionCookie), forms("presence", string(routeshape.Required), string(routeshape.Optional)))},
	{"OG-75", "fact parameters OpenAPI would misread", join(forms("presence", string(routeshape.PresenceUnknown)), forms("header", "accept", "authorization", "content-type"))},
	{"OG-76", "names read in code", join(forms("kind", string(routeshape.ReadInHandler)), forms("access", string(routeshape.ReadByHandler), string(routeshape.ReadByDependency)))},
	{"OG-77", "intended, returned and mapped items", forms("kind", string(routeshape.Intended), string(routeshape.Returned), string(routeshape.Mapped))},
	{"OG-78", "request body from facts", forms("section", routeshape.SectionBody)},
	{"OG-79", "fact body without a content type", nil},
	{"OG-80", "responses from facts", forms("section", routeshape.SectionResponses)},
	{"OG-81", "proven empty response body", nil},
	{"OG-82", "fact response body without a content type", nil},
	{"OG-83", "schema: scalar and array types", forms("type", string(routeshape.TypeString), string(routeshape.TypeNumber), string(routeshape.TypeInteger),
		string(routeshape.TypeBoolean), string(routeshape.TypeNull), string(routeshape.TypeArray), string(routeshape.TypeEnum))},
	{"OG-84", "schema: objects", join(forms("type", string(routeshape.TypeObject)), forms("additional", string(routeshape.AdditionalAllowed), string(routeshape.AdditionalForbidden), string(routeshape.AdditionalUnknown)))},
	{"OG-85", "schema: models", forms("type", string(routeshape.TypeRef))},
	{"OG-86", "schema: unknown and nullable", join(forms("type", string(routeshape.TypeUnknown)), forms("nullable", "true"))},
	{"OG-87", "security from the shape", join(forms("section", routeshape.SectionSecurity), forms("scheme", string(routeshape.SchemeHTTP), string(routeshape.SchemeAPIKey),
		string(routeshape.SchemeOpenIDConnect), string(routeshape.SchemeOAuth2), string(routeshape.SchemeUnknown)))},
	{"OG-88", "provably none", nil},
	{"OG-89", "call following", forms("stop", stopNames()...)},
	{"OG-90", "user-supplied path values: keys", forms("key", "route:", "nextjs_base_path:")},
	{"OG-91", "user-supplied path values: checks", nil},
	{"OG-92", "user-supplied path values: use", forms("resolution", SetByYou)},
	{"OG-93", "user-supplied path values: status", forms("user_value", UserValueUsed, UserValueUnused)},
	{"OG-94", "unresolved mounts: keys", forms("key", "mount:")},
	{"OG-95", "unresolved mounts: use", nil},
	{"OG-96", "unresolved mounts: shadowing", forms("shadowing", "not_checked")},
	{"D", "everything else", nil},
}

func stopNames() []string {
	out := make([]string, len(routeshape.Stops))
	for i, s := range routeshape.Stops {
		out[i] = string(s)
	}
	return out
}

package routeshape

// Row is one row of the route shape claim contract
// (docs/contract/claims/route-shape.md). Forms are the modelled values
// the row covers, written "group:value"; every constant of the model is
// the form of exactly one row (TestContractTable checks it).
type Row struct {
	ID    string
	Title string
	Forms []string
}

// Rows is the claim contract in table order. A form no row lists goes to
// the default row D: it is not stored (the section stays not_read or
// partial with a reason).
var Rows = []Row{
	{"SM-1", "attribute and model version", []string{"attribute:" + AttributeKey, "version:1"}},
	{"SM-2", "not_read", []string{"state:" + string(NotRead)}},
	{"SM-3", "provably_none", []string{"state:" + string(ProvablyNone)}},
	{"SM-4", "partial", []string{"state:" + string(Partial)}},
	{"SM-5", "complete", []string{"state:" + string(Complete)}},
	{"SM-6", "kind fact", []string{"kind:" + string(Fact)}},
	{"SM-7", "kind read_in_handler", []string{"kind:" + string(ReadInHandler)}},
	{"SM-8", "kind intended", []string{"kind:" + string(Intended)}},
	{"SM-9", "kind returned", []string{"kind:" + string(Returned)}},
	{"SM-10", "kind mapped", []string{"kind:" + string(Mapped)}},
	{"SM-11", "provenance fields and confidence", nil},
	{"SM-12", "names read in code are never required or typed", []string{"access:" + string(ReadByHandler)}},
	{"SM-13", "names read by a dependency are not declared parameters", []string{"access:" + string(ReadByDependency)}},
	{"SM-14", "required only from fact or mapped", []string{"presence:" + string(Required), "presence:" + string(Optional), "presence:" + string(PresenceUnknown)}},
	{"SM-15", "intended and returned types never become a proven schema", nil},
	{"SM-16", "parameters", []string{"access:" + string(Declared), "section:" + SectionPath, "section:" + SectionQuery, "section:" + SectionHeader, "section:" + SectionCookie}},
	{"SM-17", "request body", []string{"section:" + SectionBody}},
	{"SM-18", "responses", []string{"section:" + SectionResponses}},
	{"SM-19", "security requirements and schemes", []string{"section:" + SectionSecurity, "scheme:" + string(SchemeHTTP), "scheme:" + string(SchemeAPIKey), "scheme:" + string(SchemeOAuth2), "scheme:" + string(SchemeOpenIDConnect), "scheme:" + string(SchemeUnknown)}},
	{"SM-20", "schema language", []string{
		"type:" + string(TypeObject), "type:" + string(TypeArray), "type:" + string(TypeString), "type:" + string(TypeNumber),
		"type:" + string(TypeInteger), "type:" + string(TypeBoolean), "type:" + string(TypeNull), "type:" + string(TypeEnum),
		"type:" + string(TypeRef), "type:" + string(TypeUnknown),
		"additional:" + string(AdditionalAllowed), "additional:" + string(AdditionalForbidden), "additional:" + string(AdditionalUnknown),
	}},
	{"SM-21", "call following: proven targets only, configurable depth limit", []string{"config:index.shape_depth", "flag:--shape-depth"}},
	{"SM-22", "stop ended", []string{"stop:" + string(StopEnded)}},
	{"SM-23", "stop depth_cap", []string{"stop:" + string(StopDepthCap)}},
	{"SM-24", "stop cycle", []string{"stop:" + string(StopCycle)}},
	{"SM-25", "stop joined", []string{"stop:" + string(StopJoined)}},
	{"SM-26", "stop budget and resolver_error", []string{"stop:" + string(StopBudget), "stop:" + string(StopResolverError)}},
	{"SM-27", "stop at an unproven target", []string{"stop:" + string(StopUnresolved), "stop:" + string(StopAmbiguous), "stop:" + string(StopExternal)}},
	{"SM-28", "stop at a value that cannot be read further", []string{"stop:" + string(StopTransformed), "stop:" + string(StopOpaqueType)}},
	{"SM-29", "chain records and depth statistics", nil},
	{"D", "anything no row covers", nil},
}

// stopRows maps each stop reason to its row.
var stopRows = map[Stop]string{
	StopEnded: "SM-22", StopDepthCap: "SM-23", StopCycle: "SM-24", StopJoined: "SM-25",
	StopBudget: "SM-26", StopResolverError: "SM-26",
	StopUnresolved: "SM-27", StopAmbiguous: "SM-27", StopExternal: "SM-27",
	StopTransformed: "SM-28", StopOpaqueType: "SM-28",
}

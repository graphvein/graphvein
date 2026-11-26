package shapediff

// Row is one row of the shape differences claim contract
// (docs/contract/claims/shape-differences.md). Forms are the modelled
// values the row covers, written "group:value"; every form below is the
// form of exactly one row (TestContractTable checks it).
type Row struct {
	ID    string
	Title string
	Forms []string
}

// Forms read by the extractor (the call side). Each names the code form
// whose keys the "sends" attribute records.
const (
	FormFetchJSONBody     = "call:fetch_json_body"
	FormFetchSearchParams = "call:fetch_search_params"
	FormHeyAPIBody        = "call:heyapi_body"
	FormHeyAPIQuery       = "call:heyapi_query"
)

// Rows is the claim contract in table order. A form no row lists goes to
// the default row D: nothing is recorded or compared, and the link
// details show nothing.
var Rows = []Row{
	{"SD-1", "attribute and version", []string{"attribute:" + AttributeKey, "version:1"}},
	{"SD-2", "fetch with a literal JSON body", []string{FormFetchJSONBody}},
	{"SD-3", "fetch with literal URLSearchParams", []string{FormFetchSearchParams}},
	{"SD-4", "hey-api body type", []string{FormHeyAPIBody}},
	{"SD-5", "hey-api query type", []string{FormHeyAPIQuery}},
	{"SD-6", "route body fully read", []string{"route:body"}},
	{"SD-7", "route query fully read", []string{"route:query"}},
	{"SD-8", "sent by the frontend, not read by the handler", []string{"difference:" + SentNotRead}},
	{"SD-9", "read by the handler, not sent by this call", []string{"difference:" + ReadNotSent}},
	{"SD-10", "compared, no difference", []string{"result:no_difference"}},
	{"SD-11", "cross-repository links", []string{"link:cross_repository"}},
	{"SD-12", "where differences are shown", []string{"field:shape_differences"}},
	{"D", "anything no row covers", nil},
}

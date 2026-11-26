package server

// tryItRow is one row of the try-it claim contract
// (docs/contract/claims/try-it.md, section "Proxy and guardrails (TI)").
// The table and this list are kept 1:1 by TestTryItContractTable, and each
// row names the test that checks it.
type tryItRow struct {
	ID    string
	Topic string
	// Test is the test function that exercises the row.
	Test string
}

// Suggestion rows, also carried by every suggestion (suggestionDoc.Row).
const (
	rowSuggestCompose    = "TI-31"
	rowSuggestTraefik    = "TI-32"
	rowSuggestSpecServer = "TI-33"
	rowSuggestDockerfile = "TI-34"
	rowSuggestFrontend   = "TI-35"
	rowSuggestListen     = "TI-36"
	rowSuggestUvicorn    = "TI-37"
	rowSuggestGoListen   = "TI-38"
	rowSuggestPackage    = "TI-39"
	rowSuggestProcfile   = "TI-40"
	rowSuggestDefault    = "TI-41"
)

// tryItRows is the claim contract in table order.
var tryItRows = []tryItRow{
	{"TI-1", "endpoints", "TestTryItMethodsPerPath"},
	{"TI-2", "request origin checks", "TestTryItRequestChecks"},
	{"TI-3", "request size", "TestTryItLimits"},
	{"TI-4", "environment", "TestTryItEnvironmentsList"},
	{"TI-5", "egress destination", "TestTryItDestinationOnly"},
	{"TI-6", "egress mode", "TestTryItPurposeEveryMode"},
	{"TI-7", "URL building and the operation match", "TestTryItOperationMatch"},
	{"TI-8", "selected environment", "TestTryItSelection"},
	{"TI-9", "confirmation", "TestTryItAuthInHash"},
	{"TI-10", "production acknowledgement", "TestTryItProduction"},
	{"TI-11", "auth from an environment variable", "TestTryItAuth"},
	{"TI-12", "auth typed for the session", "TestTryItAuth"},
	{"TI-13", "headers from the browser", "TestTryItHeaders"},
	{"TI-14", "redirects", "TestTryItRedirect"},
	{"TI-15", "timeouts and response size", "TestTryItLimits"},
	{"TI-16", "concurrency", "TestTryItConcurrency"},
	{"TI-17", "TLS", "TestTryItTLS"},
	{"TI-18", "proxies", "TestTryItProxyIgnored"},
	{"TI-19", "response to the browser", "TestTryItResponseRedaction"},
	{"TI-20", "logging", "TestTryItLogging"},
	{"TI-21", "storage", "TestTryItLogging"},
	{"TI-22", "project mode", "TestTryItProject"},
	{"TI-23", "egress audit", "TestTryItDestinationOnly"},
	{"TI-24", "don't ask again for this environment", "TestTryItConfirmation"},
	{"TI-25", "switching or changing an environment", "TestTryItEnvironmentChanged"},
	{"TI-26", "free-form paths", "TestTryItFreeForm"},
	{"TI-27", "method override or body on GET and HEAD", "TestTryItUnsupported"},
	{"TI-28", "WebSocket and HTTP/2 cleartext", "TestTryItUnsupported"},
	{"TI-29", "certificate pinning", "TestTryItCertificatePin"},
	{"TI-30", "guarded environment edit", "TestTryItEnvironmentEdit"},
	{rowSuggestCompose, "suggestion: Compose published port", "TestTryItSuggestions"},
	{rowSuggestTraefik, "suggestion: Traefik router host", "TestTryItSuggestions"},
	{rowSuggestSpecServer, "suggestion: committed spec server", "TestTryItSuggestions"},
	{rowSuggestDockerfile, "suggestion: Dockerfile EXPOSE", "TestTryItSuggestions"},
	{rowSuggestFrontend, "suggestion: a member's address of this member", "TestTryItProject"},
	{rowSuggestListen, "suggestion: listen(port) literal", "TestTryItSuggestions"},
	{rowSuggestUvicorn, "suggestion: uvicorn.run(port=) literal", "TestTryItSuggestions"},
	{rowSuggestGoListen, "suggestion: Go listen address literal", "TestTryItSuggestions"},
	{rowSuggestPackage, "suggestion: package.json script port", "TestTryItSuggestions"},
	{rowSuggestProcfile, "suggestion: Procfile port", "TestTryItSuggestions"},
	{rowSuggestDefault, "suggestion: framework default", "TestTryItSuggestions"},
	{"TI-42", "CA file", "TestTryItCAFile"},
	{"TI-43", "system proxy", "TestTryItSystemProxy"},
	{"TI-44", "per-environment limits", "TestTryItEnvironmentLimits"},
	{"TI-45", "multipart form bodies", "TestTryItMultipart"},
	{"TI-46", "following a redirect once", "TestTryItRedirectFollow"},
	{"TI-47", "sending without auth", "TestTryItSendWithoutAuth"},
	{"TI-48", "environment options edit", "TestTryItEnvironmentOptions"},
	{"TI-D", "everything else", "TestTryItOperationMatch"},
}

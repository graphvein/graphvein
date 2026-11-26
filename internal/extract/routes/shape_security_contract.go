package routes

// securityShapeRow is one row of the route security claim contract
// (docs/contract/claims/route-shape-security.md). Forms are the code
// forms the row reads; each is reported as "<row>/<form>" when reached.
// TestSecurityShapeContractTable keeps the table and the document 1:1.
type securityShapeRow struct {
	ID    string
	Title string
	Forms []string
}

// securityShapeRows is the claim contract in table order. A form no row
// lists goes to D: it states no scheme.
var securityShapeRows = []securityShapeRow{
	{"SH-S1", "requirements and section state", []string{"and", "alternatives", "intended", "cap", "partial", "complete"}},
	{"SH-S2", "scheme names", []string{"http", "apiKey", "unknown"}},
	{"SH-S3", "express-jwt", []string{"default", "getToken", "optional"}},
	{"SH-S4", "express-oauth2-jwt-bearer", []string{"auth", "optional"}},
	{"SH-S5", "express-basic-auth", []string{"default"}},
	{"SH-S6", "passport.authenticate", []string{"strategy", "unregistered", "session", "callback"}},
	{"SH-S7", "passport-jwt", []string{"fromAuthHeaderAsBearerToken", "fromHeader", "fromUrlQueryParameter", "other"}},
	{"SH-S8", "passport-http-bearer", []string{"Strategy"}},
	{"SH-S9", "passport-http Basic", []string{"BasicStrategy"}},
	{"SH-S10", "express-openid-connect", []string{"requiresAuth", "requiresLoginCheck"}},
	{"SH-S11", "Express middleware read", []string{"mounted", "route", "unknown-entry"}},
	{"SH-S12", "Express complete", []string{"known", "complete"}},
	{"SH-S13", "Axum typed Bearer header", []string{"TypedHeader-Bearer", "Option"}},
	{"SH-S14", "Axum typed Basic header", []string{"TypedHeader-Basic"}},
	{"SH-S15", "tower-http header layers", []string{"bearer", "basic"}},
	{"SH-S16", "axum-login", []string{"login_required"}},
	{"SH-S17", "Axum section", []string{"partial"}},
	{"SH-S18", "gin and chi router middleware", []string{"use", "call", "order", "conditional", "jump", "chains", "gin.Default", "served-wrapper"}},
	{"SH-S19", "gin BasicAuth", []string{"BasicAuth", "BasicAuthForRealm"}},
	{"SH-S20", "chi BasicAuth", []string{"BasicAuth"}},
	{"SH-S21", "jwtauth", []string{"Verifier", "Verify", "Authenticator", "no-Authenticator"}},
	{"SH-S22", "Go complete", []string{"known", "complete"}},
	{"SH-S23", "next-auth session", []string{"getServerSession"}},
	{"SH-S24", "chi path served by another registration", []string{"shared-path"}},
	{"D", "everything else", nil},
}

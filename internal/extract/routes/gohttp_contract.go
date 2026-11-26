package routes

// Go route claim contract (task T023, ADR 0076; docs/extract/routes.md,
// section "Go"). Every claim the Go route linker (gohttp.go) makes comes
// from exactly one row of GoContract; every other form goes to the
// single default branch D and is counted per file in the go_routes_unread
// diagnostic. TestGoRoutesContract keeps this table, the task file's
// table and the docs table 1:1.

// Go contract row sides: what a row's branch does.
const (
	// goSideClaim rows have a claim branch tagged with the row id.
	goSideClaim = "claim"
	// goSideNone rows have no branch: GR-V names the assumptions.
	goSideNone = "none"
	// goSideDefault is D, the single default branch.
	goSideDefault = "default"
)

// goRow is one row of the Go route claim contract. Unit is the implementation step
// that implements the row's branch (GR2: recognition, objects,
// registrations, patterns and handlers; GR3: composition; GR4: serving
// positions; GR5: path-preserving wrappers; GR6: generated servers; GR7:
// forwarding closures and the ServeMux -> chi hop); the contract
// test checks that the fixture reaches every row of the steps implemented
// so far.
type goRow struct {
	ID   string
	Side string
	Unit string
	// Forms lists the forms of a row that names several; the branch tags
	// "<row>/<form>" and the fixture must reach every form.
	Forms []string
}

// goContract is the claim contract in the docs' order: 34 rows plus D
// (GR-K4, the zero-value ServeMux, was added by step GR2, GR-C6, the
// serving positions, by step GR4, GR-O7, path-preserving wrappers, by
// step GR5, GR-G1, oapi-codegen generated servers, by step GR6, and GR-O8,
// GR-O9 and GR-C7, forwarding closures, methods of a call result and the
// ServeMux -> chi hop, by step GR7, as reported contract changes).
var goContract = []goRow{
	{ID: "GR-K1", Side: goSideClaim, Unit: "GR2"},
	{ID: "GR-K2", Side: goSideClaim, Unit: "GR2", Forms: []string{"`http.NewServeMux()`", "`chi.NewRouter()`", "`chi.NewMux()`", "`gin.New()`", "`gin.Default()`", "`http.Handle`", "`http.HandleFunc`", "`http.DefaultServeMux`"}},
	{ID: "GR-K3", Side: goSideClaim, Unit: "GR2", Forms: []string{"`:=`", "`=`", "`var x = e`", "`var x T = e`"}},
	{ID: "GR-K4", Side: goSideClaim, Unit: "GR2", Forms: []string{"`var m http.ServeMux`", "`m := http.ServeMux{}`", "`&http.ServeMux{}`", "`new(http.ServeMux)`"}},
	{ID: "GR-O1", Side: goSideClaim, Unit: "GR2", Forms: []string{"`r.Route(p, f)`", "`r.Group(f)`"}},
	{ID: "GR-O2", Side: goSideClaim, Unit: "GR2"},
	{ID: "GR-O3", Side: goSideClaim, Unit: "GR2", Forms: []string{"`g.Group(p, mw...)`", "`g.Use(mw...)`"}},
	{ID: "GR-O4", Side: goSideClaim, Unit: "GR2"},
	{ID: "GR-O5", Side: goSideClaim, Unit: "GR2"},
	{ID: "GR-O6", Side: goSideClaim, Unit: "GR2", Forms: []string{"`x.ServeHTTP`", "`http.Handler(x)`"}},
	{ID: "GR-O7", Side: goSideClaim, Unit: "GR5", Forms: []string{"`f(x)`", "`pkg.F(x)`", "`v.M(x)`"}},
	{ID: "GR-O8", Side: goSideClaim, Unit: "GR7"},
	{ID: "GR-O9", Side: goSideClaim, Unit: "GR7"},
	{ID: "GR-R1", Side: goSideClaim, Unit: "GR2"},
	{ID: "GR-R2", Side: goSideClaim, Unit: "GR2", Forms: []string{"`Get/Post/Put/Patch/Delete/Head/Options/Connect/Trace(p, h)`", "`Handle(p, h)`", "`Method(m, p, h)`"}},
	{ID: "GR-R3", Side: goSideClaim, Unit: "GR2", Forms: []string{"`GET/POST/PUT/PATCH/DELETE/HEAD/OPTIONS(p, h...)`", "`Any(p, h...)`", "`Handle(m, p, h...)`", "`Match(ms, p, h...)`"}},
	{ID: "GR-R4", Side: goSideClaim, Unit: "GR2", Forms: []string{"`Static(p, dir)`", "`StaticFS(p, fs)`", "`StaticFile(p, file)`", "`StaticFileFS(p, file, fs)`"}},
	{ID: "GR-P1", Side: goSideClaim, Unit: "GR2", Forms: []string{"string literal", "constant", "`+`"}},
	{ID: "GR-P2", Side: goSideClaim, Unit: "GR2"},
	{ID: "GR-C1", Side: goSideClaim, Unit: "GR3"},
	{ID: "GR-C2", Side: goSideClaim, Unit: "GR3"},
	{ID: "GR-C3", Side: goSideClaim, Unit: "GR3"},
	{ID: "GR-C4", Side: goSideClaim, Unit: "GR3"},
	{ID: "GR-C5", Side: goSideClaim, Unit: "GR3"},
	{ID: "GR-C6", Side: goSideClaim, Unit: "GR4", Forms: []string{"`http.StripPrefix(p, x)`", "wrapper call", "`srv.Handler = h`"}},
	{ID: "GR-C7", Side: goSideClaim, Unit: "GR7", Forms: []string{"`m.Handle(\"/\", x)`", "forwarding closure"}},
	{ID: "GR-H1", Side: goSideClaim, Unit: "GR2"},
	{ID: "GR-H2", Side: goSideClaim, Unit: "GR2", Forms: []string{"receiver or parameter", "`var` with a type", "composite literal", "call of a module function"}},
	{ID: "GR-H3", Side: goSideClaim, Unit: "GR2"},
	{ID: "GR-H4", Side: goSideClaim, Unit: "GR2"},
	{ID: "GR-H5", Side: goSideClaim, Unit: "GR2"},
	{ID: "GR-H6", Side: goSideClaim, Unit: "GR2"},
	{ID: "GR-G1", Side: goSideClaim, Unit: "GR6", Forms: []string{"chi server", "gin server", "net/http server", "`Handler(si)`", "`HandlerFromMux(si, r)`", "`HandlerWithOptions(si, o)`", "implementing method"}},
	{ID: "GR-V", Side: goSideNone},
	{ID: "D", Side: goSideDefault, Unit: "GR2"},
}

// goUnreadForms are the counted forms of D, in the docs' order, with the
// unit that counts them.
var goUnreadForms = []struct{ Form, Unit string }{
	{"dot_import", "GR2"},
	{"unsupported_module", "GR2"},
	{"legacy_mux_patterns", "GR2"},
	{"receiver_unproven", "GR2"},
	{"callback_unread", "GR2"},
	{"param_rebound", "GR2"},
	{"factory_method_chain", "GR2"},
	{"invalid_pattern", "GR2"},
	{"dynamic_method", "GR2"},
	{"dynamic_pattern", "GR2"},
	{"regexp_param", "GR2"},
	{"mount_unproven_handler", "GR3"},
	{"wrapped_router", "GR2"},
	{"escaped_router", "GR2"},
}

// Go route D forms (goUnreadForms).
const (
	goDotImport          = "dot_import"
	goUnsupportedModule  = "unsupported_module"
	goLegacyMuxPatterns  = "legacy_mux_patterns"
	goReceiverUnproven   = "receiver_unproven"
	goCallbackUnread     = "callback_unread"
	goParamRebound       = "param_rebound"
	goFactoryMethodChain = "factory_method_chain"
	goInvalidPattern     = "invalid_pattern"
	goDynamicMethod      = "dynamic_method"
	goDynamicPattern     = "dynamic_pattern"
	goRegexpParam        = "regexp_param"
	goMountUnproven      = "mount_unproven_handler"
	goEscapedRouter      = "escaped_router"
	goWrappedRouter      = "wrapped_router"
)

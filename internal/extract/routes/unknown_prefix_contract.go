package routes

// Unknown-prefix path claim contract, route side (task T033, ADR 0084;
// docs/contract/claims/unknown-prefix-paths.md, section "Route side", and
// its copy in docs/extract/routes.md). Every unresolved_prefix_paths entry
// a route linker stores comes from exactly one claim row of
// unknownPrefixContract; every other form goes to the single default
// branch D (no entry). TestUnknownPrefixContract keeps this table, the
// public table and the docs copy 1:1, and checks one tagged branch per
// claim row of the implemented steps.

// upRow is one route-side row of the unknown-prefix contract. Unit is the
// T033 implementation step that implements the row's branch (UP-A1: Go; UP-A2:
// Express, FastAPI, Axum); the shared rows UP-8..UP-10 and D are
// implemented by each step for its frameworks. Forms lists the forms of a
// row that names several: the branch tags "<row>/<form>" and the fixture
// must reach every form.
type upRow struct {
	ID    string
	Unit  string
	Forms []string
}

// unknownPrefixContract is the route side in the public table's order,
// then D.
var unknownPrefixContract = []upRow{
	{ID: "UP-1", Unit: "UP-A1", Forms: []string{upFormRoute, upFormMount, upFormGroup}},
	{ID: "UP-2", Unit: "UP-A1", Forms: []string{upFormStrip, upFormOuter}},
	{ID: "UP-3", Unit: "UP-A1"},
	{ID: "UP-4", Unit: "UP-A1", Forms: []string{upFormWrapped, upFormEscaped}},
	{ID: "UP-5", Unit: "UP-A2"},
	{ID: "UP-6", Unit: "UP-A2", Forms: []string{upFormInclude, upFormStarlette, upFormOwnPrefix}},
	{ID: "UP-7", Unit: "UP-A2", Forms: []string{upFormNest, upFormNestService}},
	{ID: "UP-8", Unit: "UP-A1"},
	{ID: "UP-9", Unit: "UP-A1", Forms: []string{upFormTestPaths}},
	{ID: "UP-10", Unit: "UP-A1", Forms: []string{"`route_unmounted`", "the path `\"\"` below a hop"}},
	{ID: "D", Unit: "UP-A1"},
}

// upDefaultRefusals names, for every D form the linkers tag, the row
// whose refusal column states it and the words it is stated with:
// TestUnknownPrefixContract checks each against the
// public table.
var upDefaultRefusals = map[string][]upRefusal{
	"template_syntax":  {{"UP-2", "a prefix holding `{`, `}`, `*` or `:`: no entry"}},
	"no_hop":           {{"UP-1", "no hop, no entry"}, {"UP-2", "a `StripPrefix` at a serving position (GR-C6: no hop is stored there): no entry"}},
	upDefaultUnread:    {{"UP-5", "a prefix piece no row reads (`route_pattern_unmodelled`): no entry"}},
	upDefaultNotStored: {{"UP-6", "a pattern `\"\"` right at the hop (not stored below it): no entry"}, {"UP-7", "the nested `/` or a trailing-slash path on a crate whose axum version is not proven (not stored below the hop): no entry"}},
	upDefaultSlash:     {{"UP-7", "a route whose every path is left out for an unproven trailing slash (`route_trailing_slash_unproven`): no entry"}},
}

// upRefusal is one row's refusal text for a D form.
type upRefusal struct {
	Row, Text string
}

// upDefaultForms are the forms of D the Go linker tags: a StripPrefix
// prefix holding template syntax (UP-2's refusal) and a pathless route with
// no hop a row reads (a check refused its path, a prefix chi rejects, a
// StripPrefix at a serving position, a generated server's registration
// whose template is not read).
var upDefaultForms = []string{"template_syntax", "no_hop"}

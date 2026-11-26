package mcp

// claimRow is one row of the MCP server's claim contract
// (docs/contract/claims/mcp-server.md): Part names the tool or envelope
// part the row governs. TestClaimContract keeps this table and the
// document's table 1:1, and the session goldens reach every row.
type claimRow struct {
	ID   string
	Part string
}

// claimRows are the rows in table order; MC-D, the single default, is
// last.
var claimRows = []claimRow{
	{"MC-1", "envelope freshness"},
	{"MC-2", "envelope request"},
	{"MC-3", "envelope bounds"},
	{"MC-4", "envelope page"},
	{"MC-5", "envelope labels"},
	{"MC-6", "envelope omitted"},
	{"MC-7", "target resolution"},
	{"MC-8", "refusals"},
	{"MC-9", "status"},
	{"MC-10", "find"},
	{"MC-11", "endpoints"},
	{"MC-12", "endpoint"},
	{"MC-13", "source"},
	{"MC-14", "callers"},
	{"MC-15", "callers of a route"},
	{"MC-16", "impact"},
	{"MC-17", "walk ends"},
	{"MC-18", "project links"},
	{"MC-19", "read only"},
	{"MC-20", "chain"},
	{"MC-21", "chain unknowns"},
	{"MC-22", "table schema"},
	{"MC-23", "table users"},
	{"MC-24", "changes"},
	{"MC-25", "change_impact"},
	{"MC-26", "unresolved calls"},
	{"MC-27", "endpoints without a stored caller"},
	{"MC-28", "overview"},
	{"MC-29", "compact detail"},
	{"MC-30", "change bases"},
	{"MC-31", "callers and impact scope"},
	{"MC-32", "link_changes"},
	{"MC-33", "status settings"},
	{"MC-D", "default"},
}

// rowTag, when set by a test, sees every row a response used.
var rowTag func(row string)

func tagRow(row string) {
	if rowTag != nil {
		rowTag(row)
	}
}

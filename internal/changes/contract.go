// Package changes applies the change-impact claim contract
// (docs/contract/claims/change-impact.md, ADR 0056) to the raw change sets the
// store composes: it classifies every changed record by one contract row,
// labels causes and partiality, pairs inferred move hints and traverses
// the graph outward from changed facts along the contract's relation rows.
// The server, the CLI and the M3 MCP tools call it; none of them
// interpret change sets themselves.
package changes

// ContractVersion names the classification rules a comparison was made
// with, so a client can tell answers of different revisions apart:
// "T019-r8" is the task's contract with its Revisions 1, 3, 4, 7 and 8
// (Revision 8, T029 W2: the model-type relation I16 and the uses_model
// edges reported on their route, rows MT-13..MT-19).
const ContractVersion = "T019-r8"

// Row is one row of the claim contract: a cross-cutting rule (G), a change
// row (C) or the default branch (D). The table is data so that
// TestChangeClaimContract can hold it 1:1 with the task file and with the
// code: every C and D row has exactly one classifier, and every item names
// the row that produced it.
type Row struct {
	ID    string
	Group string
	Form  string
}

// Rows is the contract's row table in the task's order.
var Rows = []Row{
	{"G1", "", "keys only: file by path, node by identity, edge by kind + source + target"},
	{"G2", "", "projection: moves and L5 location nodes are counted, never listed"},
	{"G3", "", "knowledge: a side knows a record's state only outside its knowledge gaps"},
	{"G4", "", "cause: code, relink or extractor_or_relink"},
	{"G5", "", "coverage: a base the journal no longer covers is refused"},
	{"G6", "", "consistency: a comparison is bound to its head run"},
	{"G7", "", "test scope follows the Include tests switch; hidden counts shown"},
	{"G8", "", "wording: no longer in the index; deleted only where a head tree proves it"},
	{"C1", GroupFiles, "file record"},
	{"C2", GroupCode, "code symbol nodes, and their reads_table / writes_table edges (Revision 4)"},
	{"C3", GroupRoutes, "route node; its exposes_route and uses_model edges"},
	{"C4", GroupClientCalls, "client_call node"},
	{"C5", GroupClientCalls, "calls_route link between a client_call and a route"},
	{"C6", GroupAPI, "api_operation nodes, documents_route and calls_operation edges"},
	{"C7", GroupIPC, "ipc_command and ipc_call nodes, exposes_command and invokes_command edges"},
	{"C8", GroupMigrationTables, "migration-defined table"},
	{"C9", GroupMigrationTables, "migration-defined column, db_index and foreign_key edges"},
	{"C10", GroupMigrations, "migration node"},
	{"C11", GroupModelTables, "model-defined table, column and foreign keys between them"},
	{"C12", GroupAngular, "Angular ui_route and Angular structure edges"},
	{"C13", GroupArchitecture, "architecture: L1 components and their edges (not extracted yet)"},
	{"C14", "", "move hint (inferred) between one removed and one added node"},
	{"C15", GroupGaps, "knowledge gaps per file"},
	{"C16", GroupOther, "external, api_spec nodes; imports, contains, calls edges; passes_function edges not from a react-router route; a file node's reads_table / writes_table edges"},
	{"C17", GroupReact, "react-router ui_route; React renders edges, routes_to and passes_function edges from a react-router route"},
	{"C18", GroupServerActions, "server_action and action_call nodes, exposes_action and calls_action edges"},
	{"D", GroupUnclassified, "any record whose kind no row lists, or whose payload does not decode"},
}

// Relation is one impact relation row (I) or the default (ID).
type Relation struct {
	ID string
	// Wording is the claim wording of a path ending in this relation.
	Wording string
}

// Relations is the contract's relation table in the task's order.
var Relations = []Relation{
	{"I1", "calls the changed code"},
	{"I2", "makes the changed call"},
	{"I3", "calls or documents this route"},
	{"I4", "handled by the changed code"},
	{"I5", "invokes this command"},
	{"I6", "calls this operation"},
	{"I7", "references this column or alters this table"},
	{"I8", "reads, writes or builds a query on this table"},
	{"I9", "renders, injects, routes to, loads or uses this"},
	{"I10", "imports the changed file"},
	{"I11", "not extracted yet"},
	// I12 and I13 (T022 B7) follow a project's stored links into another
	// member repository; only at project scope (ImpactRequest.Project).
	{"I12", "calls this route from another project repository"},
	{"I13", "documents this route from another project repository"},
	// I14 and I15 (T026 AG-F) follow React renders, routes_to and
	// passes_function edges into function components.
	{"I14", "renders or routes to this"},
	{"I15", "passes this as callback, call not modelled"},
	// I16 (T029 W2) follows uses_model edges from a changed model
	// declaration to the routes whose shape uses it; the path's wording
	// names the model and how the route uses it (modelWording).
	{"I16", "through request/response model"},
	{"ID", "not followed"},
}

// projectRelation marks the relations that cross repositories through a
// project link. Their path wording is the relation's own, not the edge
// kind's (a project link has the kind of the in-repository edge it
// stands for).
var projectRelation = map[string]bool{"I12": true, "I13": true}

// edgeWordings are the per-edge wordings of relations whose I-table row
// names one per edge kind (I3, I7, I9; Revision 3, R3-10). A path takes
// the wording of its last edge, else its relation's; an I8 edge's wording
// also depends on its provenance and role (accessWording).
var edgeWordings = map[string]string{
	"calls_route":     "calls this route",
	"documents_route": "documents this route",
	"foreign_key":     "references this column",
	"alters":          "alters this table",
	"renders":         "renders this",
	"injects":         "injects this",
	"routes_to":       "routes to this",
	"loads_children":  "loads this",
	"uses_pipe":       "uses this",
	"uses_template":   "uses this",
}

// Groups of items, in their listing order.
const (
	GroupRoutes          = "routes"
	GroupClientCalls     = "client_calls"
	GroupAPI             = "api_operations"
	GroupIPC             = "ipc"
	GroupServerActions   = "server_actions"
	GroupMigrationTables = "migration_tables"
	GroupModelTables     = "model_tables"
	GroupMigrations      = "migrations"
	GroupCode            = "code"
	GroupAngular         = "angular"
	GroupReact           = "react"
	GroupArchitecture    = "architecture"
	GroupFiles           = "files"
	GroupGaps            = "gaps"
	GroupOther           = "other"
	GroupUnclassified    = "unclassified"
)

// GroupOrder is the stable order of groups in summaries and item pages.
var GroupOrder = []string{
	GroupRoutes, GroupClientCalls, GroupAPI, GroupIPC, GroupServerActions, GroupMigrationTables,
	GroupModelTables, GroupMigrations, GroupCode, GroupAngular,
	GroupReact, GroupArchitecture, GroupFiles, GroupGaps, GroupOther, GroupUnclassified,
}

// Change kinds (claim contract, "Change kinds").
const (
	Added       = "added"
	Removed     = "removed"
	Changed     = "changed"
	BodyChanged = "body_changed"
	UnknownNow  = "unknown_now"
	KnownNow    = "known_now"
	LinkGained  = "link_gained"
	LinkLost    = "link_lost"
	LinkUnknown = "link_unknown_now"
	// BecamePartial and Recovered are C15's kinds; Unclassified is D's.
	BecamePartial = "became_partial"
	Recovered     = "recovered"
	Unclassified  = "unclassified"
)

// Causes (G4).
const (
	CauseCode      = "code"
	CauseRelink    = "relink"
	CauseExtractor = "extractor_or_relink"
)

// Link reasons of C5 (and the C6/C7 links that use its logic).
const (
	ReasonTargetRemoved = "target_removed"
	ReasonTargetChanged = "target_changed"
	ReasonCallChanged   = "call_changed"
	// ReasonCallAbsent is a link lost because its call was removed
	// (Revision 1): the removal proves the link's absence.
	ReasonCallAbsent = "call_absent"
)

// ReasonExcluded is a removed file whose path the head tree still has
// (G8, Revision 3): ignore rules, the size limit or its language keep it
// out of the index, so it is no longer indexed, not deleted.
const ReasonExcluded = "excluded"

// Labels refining an item (besides C10's edited and verdict_changed).
const (
	// LabelHandlerChanged marks a route whose exposes_route edges changed
	// (Revision 1: the edge is reported on the route, C3).
	LabelHandlerChanged = "handler_changed"
	// LabelModelsChanged marks a route whose uses_model edges changed
	// (T029 W2, row MT-13: the route gained or lost a model declaration
	// its request or response shape uses).
	LabelModelsChanged = "models_changed"
	// LabelAltersChanged marks a migration (C10) whose alters edges
	// changed, and a listed table they connect.
	LabelAltersChanged = "alters_changed"
	// LabelTableAccessChanged marks a code symbol (C2) whose reads_table
	// or writes_table edges changed (T019 Revision 4).
	LabelTableAccessChanged = "table_access_changed"
)

// rowOf reports the contract row of id, or false.
func rowOf(id string) (Row, bool) {
	for _, r := range Rows {
		if r.ID == id {
			return r, true
		}
	}
	return Row{}, false
}

// relationOf reports the relation row of id, or false.
func relationOf(id string) (Relation, bool) {
	for _, r := range Relations {
		if r.ID == id {
			return r, true
		}
	}
	return Relation{}, false
}

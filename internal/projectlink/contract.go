package projectlink

import "strings"

// Row is one row of the claim contract table in
// docs/extract/project-links.md. The pass gives every source exactly one
// row; TestContractTable keeps this table and the documented one equal.
type Row struct {
	ID string
	// Status is the outcome status every source under this row gets.
	Status string
	// Kind is the link kind the row claims, empty for a row that claims
	// nothing.
	Kind string
}

// Outcome statuses. A status other than StatusLinked claims nothing.
const (
	StatusLinked       = "linked"
	StatusInRepository = "in_repository"
	StatusDynamic      = "dynamic"
	StatusUnproven     = "unproven"
	StatusUnmatched    = "unmatched"
	StatusIncomplete   = "incomplete"
	StatusTestOnly     = "test_only"
	StatusAmbiguous    = "ambiguous"
	StatusUnknown      = "unknown"
)

// Link kinds of the first slice (ADR 0075).
const (
	KindCallsRoute     = "calls_route"
	KindDocumentsRoute = "documents_route"
)

// Row IDs, in table order. PL-R4 (B3's "member declares link scopes"
// refusal) was removed when step B5 implemented link scopes; the ID is not
// reused.
const (
	RowScopeCall         = "PL-S1"
	RowScopeDocuments    = "PL-S2"
	RowScopeUnresolved   = "PL-S3"
	RowInRepository      = "PL-R1"
	RowDynamic           = "PL-R2"
	RowAlternatives      = "PL-R3"
	RowLiteralOrigin     = "PL-R5"
	RowNotDeclared       = "PL-R6"
	RowServerSpec        = "PL-R12"
	RowProviderUnindexed = "PL-R7"
	RowNoCandidate       = "PL-R8"
	RowUnknownPrefix     = "PL-R13"
	RowTestOnly          = "PL-R9"
	RowProviderAmbiguous = "PL-R10"
	RowSeveralProviders  = "PL-R11"
	RowCallDefault       = "PL-L1"
	RowCallUnknownPrefix = "PL-L7"
	RowCallDeclared      = "PL-L2"
	RowCallSelector      = "PL-L3"
	RowCallOrigin        = "PL-L4"
	RowDocumentsDefault  = "PL-L5"
	RowDocumentsDeclared = "PL-L6"
	RowUnknown           = "PL-U"
)

// Rows is the code side of the claim contract table, in document order.
// RowUnknown is the single default branch and comes last.
var Rows = []Row{
	{ID: RowScopeCall, Status: StatusLinked, Kind: KindCallsRoute},
	{ID: RowScopeDocuments, Status: StatusLinked, Kind: KindDocumentsRoute},
	{ID: RowScopeUnresolved, Status: StatusAmbiguous},
	{ID: RowInRepository, Status: StatusInRepository},
	{ID: RowDynamic, Status: StatusDynamic},
	{ID: RowAlternatives, Status: StatusUnproven},
	{ID: RowLiteralOrigin, Status: StatusUnmatched},
	{ID: RowNotDeclared, Status: StatusUnmatched},
	{ID: RowServerSpec, Status: StatusUnmatched},
	{ID: RowProviderUnindexed, Status: StatusIncomplete},
	{ID: RowNoCandidate, Status: StatusUnmatched},
	{ID: RowUnknownPrefix, Status: StatusUnproven},
	{ID: RowTestOnly, Status: StatusTestOnly},
	{ID: RowProviderAmbiguous, Status: StatusAmbiguous},
	{ID: RowSeveralProviders, Status: StatusAmbiguous},
	{ID: RowCallDefault, Status: StatusLinked, Kind: KindCallsRoute},
	{ID: RowCallUnknownPrefix, Status: StatusLinked, Kind: KindCallsRoute},
	{ID: RowCallDeclared, Status: StatusLinked, Kind: KindCallsRoute},
	{ID: RowCallSelector, Status: StatusLinked, Kind: KindCallsRoute},
	{ID: RowCallOrigin, Status: StatusLinked, Kind: KindCallsRoute},
	{ID: RowDocumentsDefault, Status: StatusLinked, Kind: KindDocumentsRoute},
	{ID: RowDocumentsDeclared, Status: StatusLinked, Kind: KindDocumentsRoute},
	{ID: RowUnknown, Status: StatusUnknown},
}

// row returns the table entry with id; the pass only names IDs from Rows.
func row(id string) Row {
	for _, r := range Rows {
		if r.ID == id {
			return r
		}
	}
	return Row{ID: RowUnknown, Status: StatusUnknown}
}

// ServerSpecReason is the reason of a PL-R12 outcome: an OpenAPI
// operation of a member that stores routes documents that member's own
// server, so without a consumes entry it never links to another member's
// route (T022 review).
const ServerSpecReason = "spec of a server repository"

// NoIndexedCandidate ends the reason of a PL-R7 outcome whose indexed
// candidate providers have no matching route: the source may reach only
// the unindexed provider. A PL-R7 outcome stored before candidates were
// listed has neither candidates nor this text, and views treat every
// indexed candidate provider as possible (View.PossibleCallers).
const NoIndexedCandidate = "no route of an indexed provider matches"

// UnknownPrefixReason is the reason of a PL-R13 outcome (row UP-25 of
// docs/contract/claims/unknown-prefix-paths.md, decision U5 of T033): the
// only routes that match the source are served below a prefix their code
// does not fix (unresolved_prefix_paths), and no consumes entry naming the
// provider covers the source, so nothing shows which server or which
// prefix the call reaches.
const UnknownPrefixReason = "only routes whose prefix is not read from code match (unresolved_prefix_paths); not linked across repositories without a consumes entry naming the provider"

// UnknownPrefixAssumptionNote ends the assumption of a PL-L7 link (row
// UP-26): the target's prefix is not read from code, and the link rests on
// the unknown-prefix assumptions listed (UP-V1, UP-V2 for an unresolved
// base, UP-V3 for the declaration), whose texts the public claim contract
// states.
func UnknownPrefixAssumptionNote(assumes []string) string {
	return "route prefix not read from code: assumes " + strings.Join(assumes, ", ")
}

// DefaultAssumption is shown with every link made without a declaration
// (ADR 0075 "Precision").
const DefaultAssumption = "matched across project repositories; base URL not proven"

// ScopeOverlayNote ends the assumption of a link that resolves an
// in-repository ambiguity across link scopes (rows PL-S1 and PL-S2), so a
// view says the member's own snapshot still records the call ambiguous.
const ScopeOverlayNote = "resolves an in-repository ambiguity across link scopes"

// PathAssumptionNote ends the assumption of a link whose matched route
// path entry rests on path assumptions: GR-V5 (a
// Go path served through a wrapper not proven to keep it), RO-V1 (an
// earlier mount at an unknown prefix may answer first) or RO-V2 (an
// earlier mounted app's catch-all may answer first), whose texts
// docs/extract/routes.md states. The link stores the IDs too
// (store.ProjectLink.Assumes).
func PathAssumptionNote(assumes []string) string {
	return "route path: assumes " + strings.Join(assumes, ", ")
}

// NextAssumptionNote ends the assumption of a link to another repository's
// Next.js route (TOP-1 of 2026-11-18): the Next.js row assumptions the link
// rests on besides the cross-repository one, as the in-repository linker
// records them (docs/extract/routes.md, NX-V1 and NX-V3). The link stores
// the IDs too (store.ProjectLink.Assumes).
func NextAssumptionNote(assumes []string) string {
	return "Next.js route: assumes " + strings.Join(assumes, ", ")
}

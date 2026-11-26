package server

import (
	"context"
	"fmt"
	routepaths "github.com/graphvein/graphvein/internal/extract/routes"
	"net/url"
	"sort"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/precision"
	"github.com/graphvein/graphvein/internal/store"
)

// This file holds GET /api/v0/precision and the labels the other views
// attach (ADR 0066, task T020, docs/cli/serve.md). Every scope
// label is decided by precision.ScopeLabel from a run header and a marker
// read from stored data, and every file label from the stored file record
// checked against precision.FileLabel; the UI never joins labels to groups
// itself (open point O8).

// scopeLabel is a fact family's precision label as the API serves it. Row
// is the rule-table row that decided it ("D" for the default branch);
// Reason is null for a precise label and otherwise says why the default
// branch applies (default_branch, no_run, external_indexer,
// extractor_mismatch, ...).
type scopeLabel struct {
	Label  precision.Label `json:"label"`
	Row    string          `json:"row"`
	Reason *string         `json:"reason"`
	// Unverified names the facts of a precise label's records that its
	// row does not cover (no oracle compares them): the views mark them
	// unverified beside the chip (row PL-F10: handler, middleware,
	// conditional, scope; T023 GR10). Absent otherwise.
	Unverified []string `json:"unverified,omitempty"`
}

// rowUnverified lists, per rule row, the facts of its records outside its
// Precise scope.
var rowUnverified = map[string][]string{
	"PL-F10": {"handler", "middleware", "conditional", "scope"},
}

// reasonNotCompared: a fact or edge outside its row's Precise scope (no
// oracle compares it): best effort beside a precise record (T023 GR10).
const reasonNotCompared = "not_compared"

// Server-side family markers for the views whose families have no rule
// row. They fall to the default branch through precision.ScopeLabel like
// any unknown family, so a future row only needs a matching Family value.
const (
	familyRoutes      precision.Family = precision.FamilyRoutes
	familyClientCalls precision.Family = "client_calls"
	familyIPCCommands precision.Family = "ipc_commands"
	familyIPCCalls    precision.Family = "ipc_calls"
	// familyServerActions and familyActionCalls are the server action
	// transport (T024b): no rule row names them, so they are best_effort.
	familyServerActions precision.Family = "server_actions"
	familyActionCalls   precision.Family = "action_calls"
	// familyCodeCalls is the code `calls` edges between functions and
	// methods (O-S1-5 (a) of task T020). No contract covers them in any
	// language, so they fall to the default branch; every view that shows
	// them next to a file label carries this label too, so a Precise file
	// chip (a PL-L row's structural scope) never reads as vouching for its
	// call edges.
	familyCodeCalls precision.Family = "code_calls"
	// familySchemaOther counts the schema tables of groups no row names
	// (a sqlite set, the set: null group, a conflicting tool); it is a
	// counting bucket of /api/v0/precision, never a marker.
	familySchemaOther precision.Family = "schema_other"
	// familyTableAccessOther counts the reads_table and writes_table edges
	// into tables of a group whose tool row PL-F9 does not list (Supabase
	// tables, T024c2: the SB rows have no evidence of their own yet); like
	// familySchemaOther it is a counting bucket, never a marker.
	familyTableAccessOther precision.Family = "table_access_other"
)

// accessMarker is the marker of a code-to-table link (or of an owner's
// table_access entries): the family with the target table's group tool,
// which row PL-F9's scope lists. known is false when a target is not a
// table the caller read (a removed table, a head without a graph): then
// no tool is known.
func accessMarker(markerOf func(contract.Node) precision.Marker, tables map[string]contract.Node, targets []string) (m precision.Marker, known bool) {
	m = precision.Marker{Family: precision.FamilyTableAccess}
	for i, id := range targets {
		t, ok := tables[id]
		if !ok || markerOf == nil {
			return precision.Marker{Family: precision.FamilyTableAccess}, false
		}
		g := markerOf(t)
		next := precision.Marker{Family: precision.FamilyTableAccess, Tool: g.Tool, Conflict: g.Conflict}
		if rowOf(next) == precision.DefaultRow {
			return next, true
		}
		if i == 0 {
			m = next
		}
	}
	return m, len(targets) > 0
}

// precisionRun describes the latest stored run for precision.ScopeLabel:
// built in when Graphvein's own indexer wrote it, with the header's bare
// extractor version (never the fact-cache key, which adds "/max=...") and
// this binary's index.Version. ok is false when no run is stored; the run
// is then refused as no_run, never described as an external indexer's.
func precisionRun(runs []store.Run) (run precision.Run, ok bool) {
	last := latestStoreRun(runs)
	if last == nil {
		return precision.Run{NoRun: true, BinaryVersion: index.Version}, false
	}
	return runOfIndexer(last.Header.Indexer.Name, last.Header.Indexer.Version), true
}

func runOfIndexer(name, version string) precision.Run {
	return precision.Run{BuiltIn: name == "graphvein", ExtractorVersion: version, BinaryVersion: index.Version}
}

// labelOf decides one scope label for facts of run.
func labelOf(run precision.Run, m precision.Marker) scopeLabel {
	d := precision.ScopeLabel(run, m)
	out := scopeLabel{Label: d.Label, Row: d.Row}
	if d.Reason != "" {
		reason := d.Reason
		out.Reason = &reason
	}
	if d.Label == precision.Precise {
		out.Unverified = rowUnverified[d.Row]
	}
	return out
}

// notCompared is the label of a fact outside a precise row's scope.
func notCompared() *scopeLabel {
	reason := reasonNotCompared
	return &scopeLabel{Label: precision.BestEffort, Row: precision.DefaultRow, Reason: &reason}
}

// rowOf is the row a marker falls in for an eligible run, for counting
// facts by row whatever this index's own eligibility.
func rowOf(m precision.Marker) string {
	return precision.ScopeLabel(runOfIndexer("graphvein", index.Version), m).Row
}

// schemaGroupMarker is the marker of a schema group as /schema/sets forms
// it: a model group (model records with a set key) by its tables' common
// tool; a migration set by its migrations' common tool and dialect; the
// set: null group by nothing, so it always falls to the default branch. A
// conflicting tool or dialect is empty and marks the marker Conflict, so
// the default branch says why (marker_conflict).
func schemaGroupMarker(model bool, key *string, migrations, tables []contract.Node) precision.Marker {
	switch {
	case key == nil:
		return precision.Marker{}
	case model:
		tool, conflict := commonValue(tables, "tool")
		return precision.Marker{Family: precision.FamilyModelSchema, Tool: tool, Conflict: conflict}
	case len(migrations) == 0:
		return precision.Marker{}
	}
	tool, toolConflict := commonValue(migrations, "tool")
	dialect, dialectConflict := commonValue(migrations, "dialect")
	return precision.Marker{Family: precision.FamilyMigrationSchema, Tool: tool, Dialect: dialect, Conflict: toolConflict || dialectConflict}
}

// commonValue is commonString's value, empty when absent or conflicting;
// conflict is commonString's (some records name another value or none).
func commonValue(nodes []contract.Node, key string) (value string, conflict bool) {
	v, conflict := commonString(nodes, key)
	if v == nil || conflict {
		return "", conflict
	}
	return *v, false
}

// schemaMarkers groups every table and migration the way /schema/sets
// does and returns the marker of a table's or migration's group, so a
// table detail, a change item and the fact counts carry the label the
// group shows.
func schemaMarkers(allTables, allMigrations []contract.Node) func(n contract.Node) precision.Marker {
	type group struct {
		model              bool
		key                *string
		migrations, tables []contract.Node
	}
	groups := map[string]*group{}
	groupOf := func(model bool, k *string) *group {
		id := groupID(model, k)
		if groups[id] == nil {
			groups[id] = &group{model: model, key: k}
		}
		return groups[id]
	}
	for _, m := range allMigrations {
		g := groupOf(false, groupKey(m))
		g.migrations = append(g.migrations, m)
	}
	for _, t := range allTables {
		g := groupOf(modelRecord(t) && groupKey(t) != nil, groupKey(t))
		g.tables = append(g.tables, t)
	}
	markers := map[string]precision.Marker{}
	for id, g := range groups {
		markers[id] = schemaGroupMarker(g.model, g.key, g.migrations, g.tables)
	}
	return func(n contract.Node) precision.Marker {
		k := groupKey(n)
		return markers[groupID(n.Kind == "table" && modelRecord(n) && k != nil, k)]
	}
}

// endpointFamilies are the families of a transport's endpoints and calls.
func endpointFamilies(t transport) (endpoints, calls precision.Family) {
	if t.Name == tauriTransport.Name {
		return familyIPCCommands, familyIPCCalls
	}
	if t.Name == actionTransport.Name {
		return familyServerActions, familyActionCalls
	}
	return familyRoutes, familyClientCalls
}

// endpointLabels are the scope labels of a transport's endpoints (those
// listed: one for /endpoint and /flow, all of the transport for
// /endpoints) and of its calls. The endpoints' marker names their common
// framework as its tool (row PL-F10 labels Go routes by framework, T023
// GO4); endpoints of several frameworks are a marker conflict.
//
// A precise label over routes whose shown paths include unknown-prefix
// paths (T033 rows UP-29, UP-37: best effort, no oracle compares them;
// NX-R4 entries alike) names them among its unverified facts
// (factPrefixUnknown), so a "Precise" chip never vouches for
// them.
func endpointLabels(runs []store.Run, t transport, endpoints []contract.Node) (endpointLabel, calls scopeLabel) {
	run, _ := precisionRun(runs)
	ef, cf := endpointFamilies(t)
	endpointLabel = labelOf(run, routeMarker(ef, endpoints))
	if endpointLabel.Label == precision.Precise {
		for _, n := range endpoints {
			if len(routepaths.PrefixUnknownPaths(n.Attributes)) > 0 {
				endpointLabel.Unverified = append(append([]string(nil), endpointLabel.Unverified...), factPrefixUnknown)
				break
			}
		}
	}
	return endpointLabel, labelOf(run, precision.Marker{Family: cf})
}

// factPrefixUnknown is the unverified fact of a precise route label whose
// routes show unknown-prefix paths (row UP-37).
const factPrefixUnknown = "prefix-unknown paths"

// routeMarker is the marker of a family of endpoint nodes: the family with
// the nodes' common framework (none for no node, a conflict for several).
func routeMarker(f precision.Family, nodes []contract.Node) precision.Marker {
	if f != familyRoutes || len(nodes) == 0 {
		return precision.Marker{Family: f}
	}
	tool, conflict := commonValue(nodes, "framework")
	return precision.Marker{Family: f, Tool: tool, Conflict: conflict}
}

// codeCallsLabel is the scope label of code `calls` edges for run. It is
// decided like every family label, from the family marker alone: never
// from the file label of either end.
func codeCallsLabel(run precision.Run) scopeLabel {
	return labelOf(run, precision.Marker{Family: familyCodeCalls})
}

// codeCallKinds are the node kinds code `calls` edges join: a code peek of
// one carries the code_calls label next to its file label.
var codeCallKinds = map[string]bool{"function": true, "method": true}

// fileRule is the rule table's file label of a language
// (precision.FileLabel); tests replace it to stand for a PL-L row.
var fileRule = precision.FileLabel

// fileRowOf is the rule table's file row of a language
// (precision.FileRow); tests replace it with fileRule.
var fileRowOf = precision.FileRow

// fileLabel is a file's language and precision label as the API serves
// it. Precision is Graphvein's label: the stored record's value when the
// run is eligible and the value is the one precision.FileLabel gives the
// stored language (or best_effort where the indexer refused the file's
// structure), best_effort with PrecisionReason otherwise. PrecisionRow is
// the rule row behind the label on an eligible run: the language's PL-L row
// (precise, or refused for this file) or D; null on a refused run or an
// invalid stored label. ReportedPrecision is an external
// producer's own value, never shown as Graphvein's (open point O6). All are
// null without a stored file record.
type fileLabel struct {
	Language          *string `json:"language"`
	Precision         *string `json:"precision"`
	PrecisionReason   *string `json:"precision_reason"`
	PrecisionRow      *string `json:"precision_row"`
	ReportedPrecision *string `json:"reported_precision"`
}

// reasonStoredLabelInvalid: the stored file label is not the one this
// binary's rule table gives the stored language at the same extractor
// version; the store holds a value Graphvein cannot stand behind.
const reasonStoredLabelInvalid = "stored_label_invalid"

// reasonStructureRefused: the language has a file row, but the indexer
// stored best_effort for this file because its structure claims nothing
// (for Go: go/parser rejects the file or it holds a lone CR, ADR 0068; for
// Python: the reference CPython rejects it, ADR 0069; for TypeScript, TSX
// and JavaScript: tree-sitter rejects it, ADR 0070; for Rust: tree-sitter
// reports a syntax error or it holds a lone CR, ADR 0071; or it was not
// parsed or extracted). A stored value below the rule's is never a
// guarantee the store could fake, so it is served as it is.
const reasonStructureRefused = "structure_refused"

// reasonStructureUnrooted: the language has a file row, but the indexer
// stored best_effort for this Rust file because no crate root of a read
// Cargo.toml reaches it (row PL-L4, ADR 0071): its declarations are
// claimed, its imports are not resolved as a crate member and no oracle
// configuration compiles it. The indexer records it with the diagnostic
// diagnosticStructureUnrooted, which the server reads for the reason.
const reasonStructureUnrooted = "structure_unrooted"

// diagnosticStructureUnrooted is the info diagnostic the indexer stores for
// each file it labels best_effort for reasonStructureUnrooted.
const diagnosticStructureUnrooted = "structure_unrooted"

// labelReasons is every reason a label of this API can carry: the scope
// label reasons of precision.ScopeLabel, then the server's own. It is the
// one list the UI's words are checked against: TestPrecisionReasons writes
// it to testdata/precision-reasons.golden.json, which the web tests read,
// and checks that every reason a server golden carries is in it.
func labelReasons() []string {
	return append(precision.Reasons(), reasonStoredLabelInvalid, reasonStructureRefused, reasonStructureUnrooted, reasonScopeUnknown, reasonNotCompared)
}

// fileLabelOf returns a file's label for run. unrooted reports that the
// index stores a structure_unrooted diagnostic for the file; it only
// changes the reason of a stored best_effort below a precise rule.
func fileLabelOf(rec *contract.File, run precision.Run, unrooted bool) fileLabel {
	var out fileLabel
	if rec == nil {
		return out
	}
	language, stored := rec.Language, rec.Precision
	out.Language = &language
	label, reason, row := string(precision.BestEffort), "", ""
	rule := fileRule(language)
	switch refusal := run.Refusal(); {
	case refusal != "":
		reason = refusal
		if !run.BuiltIn {
			out.ReportedPrecision = &stored
		}
	case stored == rule && stored == string(precision.Precise):
		label, row = stored, fileRowOf(language)
	case stored == rule && stored == string(precision.BestEffort):
		reason, row = precision.ReasonDefault, precision.DefaultRow
	case rule == string(precision.Precise) && stored == string(precision.BestEffort) && unrooted:
		reason, row = reasonStructureUnrooted, fileRowOf(language)
	case rule == string(precision.Precise) && stored == string(precision.BestEffort):
		reason, row = reasonStructureRefused, fileRowOf(language)
	default:
		reason = reasonStoredLabelInvalid
	}
	out.Precision = &label
	if reason != "" {
		out.PrecisionReason = &reason
	}
	if row != "" {
		out.PrecisionRow = &row
	}
	return out
}

// fileRecords maps the given paths to their stored file records, and
// reports which of them the index stores a structure_unrooted diagnostic
// for. Diagnostics are read only for records stored best_effort whose
// language has a file row, the only ones the diagnostic can explain.
func (rd *reader) fileRecords(repo string, paths []string) (map[string]*contract.File, map[string]bool, error) {
	out, unrooted := map[string]*contract.File{}, map[string]bool{}
	if len(paths) == 0 {
		return out, unrooted, nil
	}
	files, err := rd.s.FilesByPath(rd.ctx, repo, distinct(paths))
	if err != nil {
		return nil, nil, fmt.Errorf("reading file records: %w", err)
	}
	var lowered []string
	for i := range files {
		out[files[i].Path] = &files[i]
		if files[i].Precision == string(precision.BestEffort) && fileRule(files[i].Language) == string(precision.Precise) {
			lowered = append(lowered, files[i].Path)
		}
	}
	if len(lowered) == 0 {
		return out, unrooted, nil
	}
	diagnostics, err := rd.s.Diagnostics(rd.ctx, repo, lowered)
	if err != nil {
		return nil, nil, fmt.Errorf("reading file diagnostics: %w", err)
	}
	for _, d := range diagnostics {
		if d.Code == diagnosticStructureUnrooted {
			unrooted[d.File] = true
		}
	}
	return out, unrooted, nil
}

// GET /api/v0/precision

type precisionResponse struct {
	APIVersion int      `json:"api_version"`
	Repository string   `json:"repository"`
	Index      *lastRun `json:"index"`
	// BinaryExtractor is this binary's extractor version (index.Version).
	BinaryExtractor string `json:"binary_extractor"`
	// Run is the latest run's eligibility; null when nothing is indexed.
	Run     *precisionEligibility `json:"run"`
	Rules   []precisionRule       `json:"rules"`
	Default precisionDefault      `json:"default"`
	// Files counts the stored file records by language and stored label;
	// null when nothing is indexed.
	Files *precisionFiles `json:"files"`
}

type precisionEligibility struct {
	BuiltIn bool `json:"built_in"`
	// ExtractorMatches is null for an external run, whose version is the
	// producer's own and not comparable (as /status's extractor.matches).
	ExtractorMatches *bool   `json:"extractor_matches"`
	Refusal          *string `json:"refusal"`
}

type precisionRule struct {
	ID    string         `json:"id"`
	Kind  precision.Kind `json:"kind"`
	Scope string         `json:"scope"`
	// Label is what the row yields for this index: precise for an eligible
	// run, best_effort with Reason when the run is refused.
	Label  precision.Label `json:"label"`
	Reason *string         `json:"reason"`
	// Languages are, for a file row, the languages it labels; for a family
	// row, the languages of the files this index holds the row's facts in
	// (the keys of Facts.ByLanguage). No static "languages a family reads"
	// list exists in the rule table, so none is served.
	Languages []string           `json:"languages"`
	Facts     *precisionFacts    `json:"facts"`
	Marker    string             `json:"marker"`
	Match     precisionRuleMatch `json:"match"`
	Evidence  precisionEvidence  `json:"evidence"`
	Refusal   string             `json:"refusal"`
}

type precisionRuleMatch struct {
	Family   precision.Family `json:"family"`
	Tools    []string         `json:"tools"`
	Dialects []string         `json:"dialects"`
}

type precisionEvidence struct {
	Contract   []precisionTestRef  `json:"contract"`
	Goldens    []string            `json:"goldens"`
	Corpus     []precisionCitation `json:"corpus"`
	Review     []precisionCitation `json:"review"`
	Oracle     []precisionTestRef  `json:"oracle"`
	OracleNone *string             `json:"oracle_none"`
}

type precisionTestRef struct {
	Package string `json:"package"`
	Name    string `json:"name"`
}

type precisionCitation struct {
	ADR   string `json:"adr"`
	Quote string `json:"quote"`
}

// precisionFacts counts the facts this index holds in one scope, by the
// language of the file each fact is in ("none" when that file has no stored
// record). Unit says what is counted: tables for schema scopes (the group's
// tables), edges for code-to-table links (reads_table and writes_table)
// and code calls (calls), nodes for endpoint and client or IPC call
// families.
type precisionFacts struct {
	Unit       string         `json:"unit"`
	Total      int            `json:"total"`
	ByLanguage map[string]int `json:"by_language"`
}

// precisionDefault is row D of docs/extract/precision.md.
type precisionDefault struct {
	Row      string                   `json:"row"`
	Label    precision.Label          `json:"label"`
	Scope    string                   `json:"scope"`
	Covers   string                   `json:"covers"`
	Families []precisionDefaultFamily `json:"families"`
}

// precisionDefaultFamily is a family the API labels that falls to row D,
// with the facts this index holds in it.
type precisionDefaultFamily struct {
	Family    precision.Family `json:"family"`
	Label     precision.Label  `json:"label"`
	Languages []string         `json:"languages"`
	Facts     *precisionFacts  `json:"facts"`
}

// Row D's wording, kept equal to docs/extract/precision.md by
// TestPrecisionDefaultWording.
const (
	defaultScope  = "everything else"
	defaultCovers = "every file label of a language no PL-L row names (all languages but go, python, typescript, tsx, javascript and rust, including java, c_sharp, dart, html, sql, unknown); routes other than Go's `net/http`, chi and gin routes (PL-F10), client calls, code calls, cross-boundary links, cross-repository links (never precise), Angular, Tauri, OpenAPI, test scope, any external-indexer run"
)

// precisionFiles counts the stored file records by language and by
// Graphvein's label for each, decided as /source decides it (fileLabelOf,
// run refusals first), never by the stored value alone. It states what the
// store holds, not a share of "precise code" (open point O12). For an
// external run the producer's own labels are counted apart, under
// ReportedByLabel, and ReportedBy names the producer: they are never
// Graphvein's (O6).
type precisionFiles struct {
	ReportedBy *string             `json:"reported_by"`
	Total      int                 `json:"total"`
	Languages  []precisionLanguage `json:"languages"`
}

type precisionLanguage struct {
	Language string `json:"language"`
	Total    int    `json:"total"`
	// ByLabel counts the files by Graphvein's label (fileLabelOf).
	ByLabel map[string]int `json:"by_label"`
	// ReportedByLabel counts the files by the label an external producer
	// stored, not checked by Graphvein; null for a built-in run, whose
	// stored values are only Graphvein's when ByLabel agrees.
	ReportedByLabel map[string]int `json:"reported_by_label"`
}

// noFileRecord is the language key of facts whose file has no record.
const noFileRecord = "none"

func computePrecision(h *apiHandler, _ url.Values) (readFunc, error) {
	return func(ctx context.Context, rd *reader, runs []store.Run) (any, error) {
		out := precisionResponse{
			Repository: h.opt.Repository, Index: indexOf(runs), BinaryExtractor: index.Version,
			Rules:   []precisionRule{},
			Default: precisionDefault{Row: precision.DefaultRow, Label: precision.BestEffort, Scope: defaultScope, Covers: defaultCovers, Families: []precisionDefaultFamily{}},
		}
		run, indexed := precisionRun(runs)
		refusal := run.Refusal()
		if indexed {
			out.Run = &precisionEligibility{BuiltIn: run.BuiltIn}
			if run.BuiltIn {
				matches := refusal == ""
				out.Run.ExtractorMatches = &matches
			}
			if refusal != "" {
				out.Run.Refusal = &refusal
			}
		}
		var facts map[precision.Family]*precisionFacts
		var rowFacts map[string]*precisionFacts
		if indexed {
			files, err := rd.s.Files(ctx, rd.repo)
			if err != nil {
				return nil, fmt.Errorf("reading file records: %w", err)
			}
			out.Files = countFiles(files, run)
			if !run.BuiltIn {
				name := out.Index.Indexer.Name
				out.Files.ReportedBy = &name
			}
			languages := map[string]string{}
			for _, f := range files {
				languages[f.Path] = f.Language
			}
			if rowFacts, facts, err = countFacts(rd, h.opt.project, languages); err != nil {
				return nil, err
			}
		}
		for _, r := range precision.Rules() {
			row := precisionRule{
				ID: r.ID, Kind: r.Kind, Scope: r.Scope, Label: precision.Precise, Languages: nonNil(r.Languages),
				Marker: r.Marker, Refusal: r.Refusal,
				Match: precisionRuleMatch{Family: r.Match.Family, Tools: nonNil(r.Match.Tools), Dialects: nonNil(r.Match.Dialects)},
				Evidence: precisionEvidence{
					Contract: testRefs(r.Contract), Goldens: nonNil(r.Goldens),
					Corpus: citations(r.Corpus), Review: citations(r.Review), Oracle: testRefs(r.Oracle),
				},
			}
			if r.OracleNone != "" {
				none := r.OracleNone
				row.Evidence.OracleNone = &none
			}
			if refusal != "" {
				reason := refusal
				row.Label, row.Reason = precision.BestEffort, &reason
			}
			if r.Kind == precision.KindFamily && indexed {
				row.Facts = rowFacts[r.ID]
				if row.Facts == nil {
					row.Facts = &precisionFacts{Unit: unitOf(r.Match.Family), ByLanguage: map[string]int{}}
				}
				row.Languages = sortedKeys(boolKeys(row.Facts.ByLanguage))
			}
			out.Rules = append(out.Rules, row)
		}
		for _, f := range defaultFamilies {
			// Each family's label is decided like every scope label
			// (labelOf), so code_calls carries codeCallsLabel's: never
			// precise, whatever the file labels of its edges' files.
			entry := precisionDefaultFamily{Family: f, Label: labelOf(run, precision.Marker{Family: f}).Label, Languages: []string{}}
			if indexed {
				entry.Facts = facts[f]
				entry.Languages = sortedKeys(boolKeys(entry.Facts.ByLanguage))
			}
			out.Default.Families = append(out.Default.Families, entry)
		}
		return out, nil
	}, nil
}

// countFiles counts files by language and by the label fileLabelOf gives
// each for run, so the counts and /source never disagree.
func countFiles(files []contract.File, run precision.Run) *precisionFiles {
	out := &precisionFiles{Total: len(files), Languages: []precisionLanguage{}}
	byLanguage := map[string]*precisionLanguage{}
	for i := range files {
		f := &files[i]
		l := byLanguage[f.Language]
		if l == nil {
			l = &precisionLanguage{Language: f.Language, ByLabel: map[string]int{}}
			if !run.BuiltIn {
				l.ReportedByLabel = map[string]int{}
			}
			byLanguage[f.Language] = l
		}
		l.Total++
		// The label, not its reason, is counted: no diagnostic is needed.
		label := fileLabelOf(f, run, false)
		l.ByLabel[*label.Precision]++
		if label.ReportedPrecision != nil {
			l.ReportedByLabel[*label.ReportedPrecision]++
		}
	}
	for _, l := range byLanguage {
		out.Languages = append(out.Languages, *l)
	}
	sort.Slice(out.Languages, func(i, j int) bool { return out.Languages[i].Language < out.Languages[j].Language })
	return out
}

// defaultFamilies are the row D families /api/v0/precision counts, in the
// order it serves them.
var defaultFamilies = []precision.Family{familySchemaOther, familyTableAccessOther, familyRoutes, familyClientCalls, familyIPCCommands, familyIPCCalls, familyServerActions, familyActionCalls, familyCodeCalls, precision.FamilyCrossRepositoryLinks}

func unitOf(f precision.Family) string {
	switch f {
	case precision.FamilyMigrationSchema, precision.FamilyModelSchema, familySchemaOther:
		return "tables"
	case precision.FamilyTableAccess, familyTableAccessOther, familyCodeCalls:
		return "edges"
	case precision.FamilyCrossRepositoryLinks:
		return "links"
	}
	return "nodes"
}

// countFacts counts the facts of every rule row and of the default
// families by file language. Schema tables are counted by the row their
// group's marker falls in (familySchemaOther when none); code-to-table
// links are the reads_table and writes_table edges into stored tables,
// counted by the row their target's group tool falls in
// (familyTableAccessOther when none), and code calls (familyCodeCalls) every `calls` edge, both by the
// language of the edge source's file; in project mode (ps not nil) the
// cross-repository links with an end in this member (countProjectLinks).
func countFacts(rd *reader, ps *projectState, languages map[string]string) (map[string]*precisionFacts, map[precision.Family]*precisionFacts, error) {
	rows := map[string]*precisionFacts{}
	families := map[precision.Family]*precisionFacts{}
	add := func(dst *precisionFacts, file string) {
		l, ok := languages[file]
		if !ok {
			l = noFileRecord
		}
		dst.ByLanguage[l]++
		dst.Total++
	}
	byRow := func(row string, f precision.Family) *precisionFacts {
		if rows[row] == nil {
			rows[row] = &precisionFacts{Unit: unitOf(f), ByLanguage: map[string]int{}}
		}
		return rows[row]
	}
	for _, f := range defaultFamilies {
		families[f] = &precisionFacts{Unit: unitOf(f), ByLanguage: map[string]int{}}
	}
	tables, err := rd.nodes("table")
	if err != nil {
		return nil, nil, err
	}
	migrations, err := rd.nodes("migration")
	if err != nil {
		return nil, nil, err
	}
	markerOf := schemaMarkers(tables, migrations)
	for _, t := range tables {
		m := markerOf(t)
		if row := rowOf(m); row != precision.DefaultRow {
			add(byRow(row, m.Family), t.File)
		} else {
			add(families[familySchemaOther], t.File)
		}
	}
	access, err := rd.edgeKinds(identities(tables), []string{"reads_table", "writes_table"}, store.Incoming)
	if err != nil {
		return nil, nil, err
	}
	byID := map[string]contract.Node{}
	for _, t := range tables {
		byID[t.Identity] = t
	}
	for _, e := range access {
		_, file, _, err := contract.ParseIdentity(e.Source)
		if err != nil {
			file = ""
		}
		m, _ := accessMarker(markerOf, byID, []string{e.Target})
		if row := rowOf(m); row != precision.DefaultRow {
			add(byRow(row, precision.FamilyTableAccess), file)
		} else {
			add(families[familyTableAccessOther], file)
		}
	}
	for kind, f := range map[string]precision.Family{"route": familyRoutes, "client_call": familyClientCalls, "ipc_command": familyIPCCommands, "ipc_call": familyIPCCalls, "server_action": familyServerActions, "action_call": familyActionCalls} {
		nodes, err := rd.nodes(kind)
		if err != nil {
			return nil, nil, err
		}
		for _, n := range nodes {
			if f == familyRoutes {
				m := routeMarker(f, []contract.Node{n})
				if row := rowOf(m); row != precision.DefaultRow {
					add(byRow(row, f), n.File)
					continue
				}
			}
			add(families[f], n.File)
		}
	}
	calls, err := rd.codeCalls()
	if err != nil {
		return nil, nil, err
	}
	for _, e := range calls {
		_, file, _, err := contract.ParseIdentity(e.Source)
		if err != nil {
			file = ""
		}
		add(families[familyCodeCalls], file)
	}
	if err := countProjectLinks(rd, ps, families[precision.FamilyCrossRepositoryLinks], add); err != nil {
		return nil, nil, err
	}
	return rows, families, nil
}

// countProjectLinks counts, in project mode, the stored project links with
// an end in this member (T022 B11), by the language of that end's file: a
// link between two members is counted once by each. Outside project mode
// the family is listed with no facts, so the table always names it.
func countProjectLinks(rd *reader, ps *projectState, dst *precisionFacts, add func(*precisionFacts, string)) error {
	if ps == nil {
		return nil
	}
	view, err := ps.view(rd.ctx)
	if err != nil {
		return err
	}
	for _, l := range view.Set.Links {
		end := l.Target
		switch {
		case l.Source.Repository == rd.repo:
			end = l.Source
		case l.Target.Repository != rd.repo:
			continue
		}
		_, file, _, err := contract.ParseIdentity(end.Identity)
		if err != nil {
			file = ""
		}
		add(dst, file)
	}
	return nil
}

// codeCalls reads every code `calls` edge by kind (ADR 0073). Their
// sources are functions and methods, but also files, classes and
// variables (module-level calls), so no list of source kinds would be
// sure to find them all.
func (rd *reader) codeCalls() ([]contract.Edge, error) {
	edges, err := rd.s.EdgesOfKind(rd.ctx, rd.repo, "calls")
	if err != nil {
		return nil, fmt.Errorf("reading calls edges: %w", err)
	}
	return edges, nil
}

func boolKeys(m map[string]int) map[string]bool {
	out := map[string]bool{}
	for k := range m {
		out[k] = true
	}
	return out
}

func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

func testRefs(refs []precision.TestRef) []precisionTestRef {
	out := make([]precisionTestRef, 0, len(refs))
	for _, r := range refs {
		out = append(out, precisionTestRef{Package: r.Package, Name: r.Name})
	}
	return out
}

func citations(cites []precision.Citation) []precisionCitation {
	out := make([]precisionCitation, 0, len(cites))
	for _, c := range cites {
		out = append(out, precisionCitation{ADR: c.ADR, Quote: c.Quote})
	}
	return out
}

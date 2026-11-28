package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/query"
	"github.com/graphvein/graphvein/internal/store"
)

// This file holds the schema endpoints (ADR 0048, docs/cli/serve.md):
// GET /api/v0/schema/summary, /api/v0/schema/sets, /api/v0/schema/table and
// /api/v0/schema/migration. They read the schema records T018a extracts
// from sqlx migrations, return stored attributes verbatim, and derive a few
// fields by the rules in schemadata.go. Like the API section endpoints they
// are read-only and uncached.

// GET /api/v0/schema/summary

type schemaSummaryResponse struct {
	APIVersion    int      `json:"api_version"`
	Repository    string   `json:"repository"`
	Index         *lastRun `json:"index"`
	Found         string   `json:"found"`
	UnknownReason *string  `json:"unknown_reason"`
	Sets          *int     `json:"sets"`
	TestOnlySets  *int     `json:"test_only_sets"`
	FailedSets    *int     `json:"failed_sets"`
	// SetsWithoutRecords and UnreadableManifests count section 2's lists of
	// the same names (T018b1 review).
	SetsWithoutRecords  *int `json:"sets_without_records"`
	UnreadableManifests *int `json:"unreadable_manifests"`
	// ScanGaps counts section 2's scan_gaps: paths the scanner did not read
	// that may hide a manifest (T018b1 review).
	ScanGaps *int `json:"scan_gaps"`
	// ModelGroups counts the model groups (T018f); SourcesRead lists the
	// sources whose pass the index's version proves ran; UnreadTools the
	// migration tools found among the stored paths that no pass reads.
	ModelGroups *int               `json:"model_groups"`
	SourcesRead []string           `json:"sources_read"`
	UnreadTools []schemaUnreadTool `json:"unread_tools"`
	// RefusedSources are the model sources a linker found and refused as a
	// whole (T070): the reason the index holds no model table for them.
	RefusedSources []schemaRefusedSource `json:"refused_sources"`
}

func computeSchemaSummary(h *apiHandler, _ url.Values) (readFunc, error) {
	return func(ctx context.Context, rd *reader, runs []store.Run) (any, error) {
		migrations, err := rd.nodes("migration")
		if err != nil {
			return nil, err
		}
		// One file list per request: the manifests and the unread tools
		// both derive from it (F7: each read of it costs ~10 ms on langflow).
		hashes, err := rd.fileHashes()
		if err != nil {
			return nil, err
		}
		manifests, diags, err := rd.manifestDiagnostics(hashes)
		if err != nil {
			return nil, err
		}
		unfiled, err := rd.unfiledDiagnostics()
		if err != nil {
			return nil, err
		}
		bySet := map[string][]contract.Node{}
		for _, m := range migrations {
			if k := groupKey(m); k != nil {
				bySet[*k] = append(bySet[*k], m)
			}
		}
		sets, testSets := len(bySet), 0
		for _, list := range bySet {
			if t := testOnly(list); t != nil && *t {
				testSets++
			}
		}
		tables, err := rd.nodes("table")
		if err != nil {
			return nil, err
		}
		modelSets := map[string]bool{}
		for _, t := range tables {
			if k := groupKey(t); modelRecord(t) && k != nil {
				modelSets[*k] = true
			}
		}
		modelGroups := len(modelSets)
		failedCount := len(failedSets(diags))
		withoutCount := len(setsWithoutRecords(manifests, migrations, diags))
		unreadableCount := len(unreadableManifests(append(diags, unfiled...)))
		gapCount := len(scanGaps(unfiled))
		last := indexOf(runs)
		out := schemaSummaryResponse{Repository: h.opt.Repository, Index: last}
		out.Found, out.UnknownReason = foundState(last, sets+failedCount+withoutCount+modelGroups, unreadableCount, gapCount)
		if last != nil {
			out.Sets, out.TestOnlySets, out.FailedSets = &sets, &testSets, &failedCount
			out.SetsWithoutRecords, out.UnreadableManifests, out.ScanGaps = &withoutCount, &unreadableCount, &gapCount
			out.ModelGroups = &modelGroups
		}
		out.SourcesRead = sourcesRead(last)
		if out.UnreadTools, err = rd.unreadTools(hashes, last); err != nil {
			return nil, err
		}
		if out.RefusedSources, err = rd.refusedSources(hashes, last); err != nil {
			return nil, err
		}
		return out, nil
	}, nil
}

// GET /api/v0/schema/sets

type schemaSetsResponse struct {
	APIVersion int               `json:"api_version"`
	Repository string            `json:"repository"`
	Index      *lastRun          `json:"index"`
	Sets       []schemaSet       `json:"sets"`
	FailedSets []schemaFailedSet `json:"failed_sets"`
	// SetsWithoutRecords are sets T018a found but emitted no record for,
	// such as a set whose every member is unreadable (T018b1 review).
	SetsWithoutRecords []schemaRecordlessSet `json:"sets_without_records"`
	// UnreadableManifests are manifests the pass could not read, so whether
	// they declare a set is unknown (T018b1 review).
	UnreadableManifests []schemaUnreadableManifest `json:"unreadable_manifests"`
	// ScanGaps are the scanner's diagnostics on paths it did not read that
	// may hide a manifest; with one, no set list is proven complete
	// (T018b1 review).
	ScanGaps []schemaScanGap `json:"scan_gaps"`
	// UnreadTools is the summary's list (T018f 1.3), so one response
	// holds everything the set strip shows.
	UnreadTools []schemaUnreadTool `json:"unread_tools"`
	// ModelListReasons are the B1 reasons (python_unread, python_scan_gap)
	// that hold for the whole repository, so the "No model table claimed"
	// card can name them when there is no model group; null when the model
	// pass did not run (T018f Revision 2, F6 decision b).
	ModelListReasons []string `json:"model_list_reasons"`
}

// schemaSet is one group of tables: a migration set, keyed by its stored
// set directory, or the set: null group of records without a set.
type schemaSet struct {
	Set               *string  `json:"set"`
	Source            *string  `json:"source"`
	Tool              *string  `json:"tool"`
	Package           *string  `json:"package"`
	Dialect           *string  `json:"dialect"`
	Conflicts         []string `json:"conflicts"`
	Objects           *string  `json:"objects"`
	NotModeledReasons []string `json:"not_modeled_reasons"`
	TableList         *string  `json:"table_list"`
	TableListReasons  []string `json:"table_list_reasons"`
	OpaqueTotal       any      `json:"opaque_total"`
	OpaqueByKind      any      `json:"opaque_by_kind"`
	TestOnly          *bool    `json:"test_only"`
	// TablesByScope counts the group's tables by scope (T018f O3).
	TablesByScope    map[string]int `json:"tables_by_scope"`
	DiagnosticCounts map[string]int `json:"diagnostic_counts"`
	// RestsOn is the union of the assumptions behind the group's facts
	// (T018f); null when a record predates the claims.
	RestsOn []string `json:"rests_on"`
	// Platform is the environment assumption of a set whose migrations
	// rely on platform objects they never create (T024c1: Supabase,
	// assumption SB-V2, with the objects listed); omitted otherwise.
	Platform any `json:"platform,omitempty"`
	// Precision is the group's scope label (T020, ADR 0066): a rule-table
	// row for a migration set or model group it names, the default branch
	// otherwise and for the set: null group.
	Precision scopeLabel `json:"precision"`
	// schemaModelFields is set on model groups only.
	*schemaModelFields
	Migrations        []schemaSetMigration `json:"migrations"`
	Tables            []schemaSetTable     `json:"tables"`
	ForeignKeys       []schemaForeignKey   `json:"foreign_keys"`
	Unattached        schemaUnattached     `json:"unattached"`
	LinkedWithoutEdge int                  `json:"linked_without_edge"`
}

type schemaSetMigration struct {
	ID         string         `json:"id"`
	File       string         `json:"file"`
	Attributes map[string]any `json:"attributes"`
	Creates    []string       `json:"creates"`
	Alters     []string       `json:"alters"`
}

type schemaSetTable struct {
	Node                 contract.Node     `json:"node"`
	Completeness         string            `json:"completeness"`
	CreatedBy            *string           `json:"created_by"`
	AlteredBy            []string          `json:"altered_by"`
	Columns              []schemaSetColumn `json:"columns"`
	Indexes              []contract.Node   `json:"indexes"`
	ForeignKeysOut       int               `json:"foreign_keys_out"`
	ForeignKeysIn        int               `json:"foreign_keys_in"`
	UnresolvedReferences int               `json:"unresolved_references"`
	// ExternalReferences counts references to platform objects outside
	// the set (T024c1 SB-M2: Supabase's auth.users, storage tables);
	// omitted when there are none.
	ExternalReferences int `json:"external_references,omitempty"`
	// RestsOn, Overlaps and OpaqueSites are T018f's: the assumptions every
	// fact of the table rests on, the same-name tables of the other
	// source, and the opaque statements behind opaque_after.
	RestsOn     []string           `json:"rests_on"`
	Overlaps    []schemaOverlap    `json:"overlaps"`
	OpaqueSites []schemaOpaqueSite `json:"opaque_sites"`
	// VerificationCounts counts a model table's column claims by
	// verification ("oracle", "mapped"), so the list can mark a table with
	// a fact that is not oracle-verified without the claims themselves,
	// which only /table serves (T018g0 Revision 1). Null on a migration
	// table and on a model table whose columns predate the claims.
	VerificationCounts map[string]int `json:"verification_counts"`
}

type schemaSetColumn struct {
	Node         contract.Node `json:"node"`
	DefaultState string        `json:"default_state"`
}

type schemaForeignKey struct {
	SourceTable  string  `json:"source_table"`
	SourceColumn string  `json:"source_column"`
	TargetTable  *string `json:"target_table"`
	TargetColumn string  `json:"target_column"`
	Edge         edgeRef `json:"edge"`
}

type schemaUnattached struct {
	Columns int `json:"columns"`
	Indexes int `json:"indexes"`
}

type schemaFailedSet struct {
	Package string `json:"package"`
	Message string `json:"message"`
}

type schemaRecordlessSet struct {
	Package          string         `json:"package"`
	DiagnosticCounts map[string]int `json:"diagnostic_counts"`
}

type schemaUnreadableManifest struct {
	File string `json:"file"`
	// Code is the diagnostic the entry comes from: schema_manifest_unreadable,
	// or the scanner's symlink or file_read.
	Code    string `json:"code"`
	Message string `json:"message"`
}

type schemaScanGap struct {
	File    string `json:"file"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func computeSchemaSets(h *apiHandler, _ url.Values) (readFunc, error) {
	return func(ctx context.Context, rd *reader, runs []store.Run) (any, error) {
		out := schemaSetsResponse{Repository: h.opt.Repository, Index: indexOf(runs), Sets: []schemaSet{}}
		var g schemaGraph
		var err error
		if g.migrations, err = rd.nodes("migration"); err != nil {
			return nil, err
		}
		if g.tables, err = rd.nodes("table"); err != nil {
			return nil, err
		}
		hashes, err := rd.fileHashes()
		if err != nil {
			return nil, err
		}
		manifests := manifestsOf(hashes)
		if len(g.migrations) > 0 || len(g.tables) > 0 {
			if g.columns, err = rd.columnsWithoutModelClaims(); err != nil {
				return nil, err
			}
			if g.indexes, err = rd.nodes("db_index"); err != nil {
				return nil, err
			}
			if g.migrationEdges, err = rd.edgeKinds(identities(g.migrations), []string{"contains", "alters"}, store.Outgoing); err != nil {
				return nil, err
			}
			if g.tableEdges, err = rd.edgeKinds(identities(g.tables), []string{"contains"}, store.Outgoing); err != nil {
				return nil, err
			}
			if g.foreignKeys, err = rd.edgeKinds(identities(g.columns), []string{"foreign_key"}, store.Outgoing); err != nil {
				return nil, err
			}
		}
		files := append(setFiles(g.migrations), manifests...)
		for _, m := range manifests {
			files = append(files, sqlxConfig(m))
		}
		if g.diagnostics, err = rd.diagnostics(files); err != nil {
			return nil, err
		}
		out.Index = indexOf(runs)
		g.read = sourcesRead(out.Index)
		if out.UnreadTools, err = rd.unreadTools(hashes, out.Index); err != nil {
			return nil, err
		}
		if hasModelTables(g.tables) || slices.Contains(g.read, sourceModels) {
			if g.modelDiagnostics, err = rd.modelDiagnostics(hashes); err != nil {
				return nil, err
			}
		}
		g.unreadTools = out.UnreadTools
		if g.unfiled, err = rd.unfiledDiagnostics(); err != nil {
			return nil, err
		}
		if slices.Contains(g.read, sourceModels) {
			out.ModelListReasons = pythonListReasons(g.modelDiagnostics, g.unfiled)
		}
		g.run, _ = precisionRun(runs)
		if len(g.migrations) > 0 || len(g.tables) > 0 {
			out.Sets = buildSets(g)
		}
		out.FailedSets = failedSets(g.diagnostics)
		out.SetsWithoutRecords = setsWithoutRecords(manifests, g.migrations, g.diagnostics)
		out.UnreadableManifests = unreadableManifests(append(g.diagnostics, g.unfiled...))
		out.ScanGaps = scanGaps(g.unfiled)
		return out, nil
	}, nil
}

// GET /api/v0/schema/table

type schemaTableResponse struct {
	APIVersion int       `json:"api_version"`
	Repository string    `json:"repository"`
	Index      *lastRun  `json:"index"`
	IDE        ideStatus `json:"ide"`
	Set        *string   `json:"set"`
	Dialect    *string   `json:"dialect"`
	// Dialects are a model table's run-time dialects (ADR 0065), omitted
	// for every other table.
	Dialects             []string            `json:"dialects,omitempty"`
	Table                presentedNode       `json:"table"`
	Completeness         string              `json:"completeness"`
	Columns              []schemaTableColumn `json:"columns"`
	Indexes              []schemaTableIndex  `json:"indexes"`
	History              []schemaEvent       `json:"history"`
	Reasons              []any               `json:"reasons"`
	Neighbours           schemaNeighbours    `json:"neighbours"`
	UnresolvedReferences []schemaUnresolved  `json:"unresolved_references"`
	// ExternalReferences are the references to platform objects outside
	// the set (T024c1 SB-M2); omitted when there are none.
	ExternalReferences []schemaUnresolved `json:"external_references,omitempty"`
	Truncated          *schemaTruncated   `json:"truncated"`
	// T018f: the assumptions every fact rests on, the opaque statements
	// behind opaque_after and the same-name tables of the other source.
	RestsOn     []string             `json:"rests_on"`
	OpaqueSites []schemaLinkedSite   `json:"opaque_sites"`
	Overlaps    []schemaTableOverlap `json:"overlaps"`
	// Precision is the scope label of the table's /schema/sets group.
	Precision scopeLabel `json:"precision"`
	// ModelRoutes (model tables only; T029 rows MT-20..MT-29) lists the
	// routes whose stored shape accepts or returns the table's model, the
	// same declaration node; absent for a migration table.
	ModelRoutes *modelRoutesDoc `json:"model_routes,omitempty"`
}

// schemaLinkedSite is an opaque site with its migration and IDE link.
type schemaLinkedSite struct {
	schemaOpaqueSite
	MigrationRef *schemaReasonRef `json:"migration_ref"`
	Link         *query.IDELink   `json:"link"`
}

// schemaTableOverlap is an overlap with the other table's completeness and
// name.
type schemaTableOverlap struct {
	schemaOverlap
	Completeness string `json:"completeness"`
	Name         any    `json:"name"`
}

type schemaTableColumn struct {
	Node         presentedNode      `json:"node"`
	DefaultState string             `json:"default_state"`
	ForeignKeys  []schemaColumnLink `json:"foreign_keys"`
	// Claims are the stored claims with each statement's migration and
	// IDE link (T018f); null when the record has none (an older index).
	Claims []map[string]any `json:"claims"`
}

// schemaColumnLink is one stored foreign_key edge of a column, with the
// edge's evidence verbatim.
type schemaColumnLink struct {
	TargetColumn string           `json:"target_column"`
	TargetTable  *string          `json:"target_table"`
	Edge         schemaEdgeDetail `json:"edge"`
}

type schemaEdgeDetail struct {
	edgeRef
	Evidence []contract.Evidence `json:"evidence"`
}

type schemaTableIndex struct {
	Node   presentedNode    `json:"node"`
	Claims []map[string]any `json:"claims"`
}

// schemaMigrationRef names a migration by its raw attributes.
type schemaMigrationRef struct {
	ID          string `json:"id"`
	File        string `json:"file"`
	Version     any    `json:"version"`
	Order       any    `json:"order"`
	Description any    `json:"description"`
}

type schemaEvent struct {
	Migration schemaMigrationRef  `json:"migration"`
	Event     string              `json:"event"`
	Evidence  []contract.Evidence `json:"evidence"`
}

// schemaReasonRef is the migration_ref of a partial reason.
type schemaReasonRef struct {
	ID      string `json:"id"`
	File    string `json:"file"`
	Version any    `json:"version"`
	Order   any    `json:"order"`
}

type schemaNeighbours struct {
	Outgoing []schemaNeighbour `json:"outgoing"`
	Incoming []schemaNeighbour `json:"incoming"`
}

type schemaNeighbour struct {
	Table        contract.Node   `json:"table"`
	Completeness string          `json:"completeness"`
	Pairs        []neighbourPair `json:"pairs"`
}

type schemaUnresolved struct {
	Column    string `json:"column"`
	Reference any    `json:"reference"`
}

type schemaTruncated struct {
	TablesOmitted int      `json:"tables_omitted"`
	PairsOmitted  int      `json:"pairs_omitted"`
	Reasons       []string `json:"reasons"`
}

// schemaID parses the id parameter of a detail endpoint: a canonical
// identity of the given kind.
func schemaID(q url.Values, want string) (string, error) {
	id, present, err := param(q, "id")
	if err != nil {
		return "", err
	}
	if !present || id == "" {
		return "", badRequest("parameter id is required")
	}
	kind, _, _, err := contract.ParseIdentity(id)
	if err != nil {
		return "", badRequest("id %q is not a canonical node identity", id)
	}
	if kind != want {
		return "", badRequest("id %q is a %s, not a %s", id, kind, want)
	}
	return id, nil
}

func notFound(what, id string) error {
	return &apiError{http.StatusNotFound, "not_found", fmt.Sprintf("no %s with identity %s", what, id)}
}

func computeSchemaTable(h *apiHandler, q url.Values) (readFunc, error) {
	id, err := schemaID(q, "table")
	if err != nil {
		return nil, err
	}
	editor, err := editorParam(q)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, rd *reader, runs []store.Run) (any, error) {
		found, err := rd.byIdentity([]string{id})
		if err != nil {
			return nil, err
		}
		table, ok := found[id]
		if !ok || table.Kind != "table" {
			return nil, notFound("table", id)
		}
		incoming, err := rd.edgeKinds([]string{id}, []string{"contains", "alters"}, store.Incoming)
		if err != nil {
			return nil, err
		}
		outgoing, err := rd.edgeKinds([]string{id}, []string{"contains"}, store.Outgoing)
		if err != nil {
			return nil, err
		}
		reasons := objectsOrRaw(table.Attributes["reasons"])
		want := []string{}
		for _, e := range incoming {
			want = append(want, e.Source)
		}
		for _, e := range outgoing {
			want = append(want, e.Target)
		}
		for _, r := range reasons {
			if m, isObject := r.(map[string]any); isObject {
				if s, isString := m["migration"].(string); isString {
					want = append(want, s)
				}
			}
		}
		allMigrations, err := rd.nodes("migration")
		if err != nil {
			return nil, err
		}
		related, err := rd.byIdentity(want)
		if err != nil {
			return nil, err
		}
		for _, m := range allMigrations {
			related[m.Identity] = m
		}
		var columns, indexes []contract.Node
		for _, e := range outgoing {
			n, stored := related[e.Target]
			switch {
			case stored && n.Kind == "column":
				columns = append(columns, n)
			case stored && n.Kind == "db_index":
				indexes = append(indexes, n)
			}
		}
		sort.Slice(columns, func(i, j int) bool { return lessByPosition(columns[i], columns[j]) })
		sort.Slice(indexes, func(i, j int) bool { return lessByQualifiedName(indexes[i], indexes[j]) })
		columnIDs := identities(columns)
		fkOut, err := rd.edgeKinds(columnIDs, []string{"foreign_key"}, store.Outgoing)
		if err != nil {
			return nil, err
		}
		fkIn, err := rd.edgeKinds(columnIDs, []string{"foreign_key"}, store.Incoming)
		if err != nil {
			return nil, err
		}
		others := []string{}
		for _, e := range fkOut {
			others = append(others, e.Target)
		}
		for _, e := range fkIn {
			others = append(others, e.Source)
		}
		holders, err := rd.edgeKinds(distinct(others), []string{"contains"}, store.Incoming)
		if err != nil {
			return nil, err
		}
		holderIDs := []string{}
		for _, e := range holders {
			holderIDs = append(holderIDs, e.Source)
		}
		neighbourNodes, err := rd.byIdentity(holderIDs)
		if err != nil {
			return nil, err
		}
		// The table of a column: the first source by identity of its
		// incoming contains edges that is a stored table node.
		tableOf := map[string]string{}
		for _, e := range holders {
			n, stored := neighbourNodes[e.Source]
			if !stored || n.Kind != "table" {
				continue
			}
			if cur, seen := tableOf[e.Target]; !seen || e.Source < cur {
				tableOf[e.Target] = e.Source
			}
		}
		hashes, err := rd.fileHashes()
		if err != nil {
			return nil, err
		}
		allTables, err := rd.nodes("table")
		if err != nil {
			return nil, err
		}
		unread, err := rd.unreadTools(hashes, indexOf(runs))
		if err != nil {
			return nil, err
		}
		// A migration table's overlaps need every model table listed
		// (overlapsProven); a model table's do not read this.
		modelsComplete := false
		if read := sourcesRead(indexOf(runs)); !modelRecord(table) && slices.Contains(read, sourceModels) {
			pyDiags, err := rd.modelDiagnostics(hashes)
			if err != nil {
				return nil, err
			}
			unfiled, err := rd.unfiledDiagnostics()
			if err != nil {
				return nil, err
			}
			modelsComplete = modelListsComplete(read, modelGroupsOf(allTables), pyDiags, unfiled)
		}
		p := h.newPresenter(hashes, editor)
		c := tableCompleteness(table)
		out := schemaTableResponse{
			Repository: h.opt.Repository, Index: indexOf(runs), IDE: p.ide,
			Set: stringValue(table.Attributes["set"]), Dialect: stringValue(table.Attributes["dialect"]),
			Dialects: tableDialects(table), Table: p.node(servedTable(table)), Completeness: c, Columns: []schemaTableColumn{}, Indexes: []schemaTableIndex{},
			History: []schemaEvent{}, Reasons: []any{}, UnresolvedReferences: []schemaUnresolved{},
			Neighbours: schemaNeighbours{Outgoing: []schemaNeighbour{}, Incoming: []schemaNeighbour{}},
		}
		linksOf := map[string][]schemaColumnLink{}
		outPairs, inPairs := map[string][]neighbourPair{}, map[string][]neighbourPair{}
		for _, e := range fkOut {
			link := schemaColumnLink{TargetColumn: e.Target, Edge: schemaEdgeDetail{edgeRef: refOf(e), Evidence: evidenceOf(e)}}
			if t, ok := tableOf[e.Target]; ok {
				link.TargetTable = &t
				outPairs[t] = append(outPairs[t], neighbourPair{SourceColumn: e.Source, TargetColumn: e.Target})
			}
			linksOf[e.Source] = append(linksOf[e.Source], link)
		}
		for _, e := range fkIn {
			if t, ok := tableOf[e.Source]; ok {
				inPairs[t] = append(inPairs[t], neighbourPair{SourceColumn: e.Source, TargetColumn: e.Target})
			}
		}
		for _, col := range columns {
			links := linksOf[col.Identity]
			if links == nil {
				links = []schemaColumnLink{}
			}
			sort.Slice(links, func(i, j int) bool { return links[i].TargetColumn < links[j].TargetColumn })
			out.Columns = append(out.Columns, schemaTableColumn{Node: p.node(col), DefaultState: columnDefaultState(col, c), ForeignKeys: links, Claims: p.claims(col, related)})
			for _, r := range columnReferences(col) {
				if r.External {
					out.ExternalReferences = append(out.ExternalReferences, schemaUnresolved{Column: col.Identity, Reference: r.Entry})
					continue
				}
				if !r.Linked {
					out.UnresolvedReferences = append(out.UnresolvedReferences, schemaUnresolved{Column: col.Identity, Reference: r.Entry})
				}
			}
		}
		for _, ix := range indexes {
			out.Indexes = append(out.Indexes, schemaTableIndex{Node: p.node(ix), Claims: p.claims(ix, related)})
		}
		out.History = tableHistory(table, incoming, related)
		for _, r := range reasons {
			m, isObject := r.(map[string]any)
			if !isObject {
				out.Reasons = append(out.Reasons, r)
				continue
			}
			entry := map[string]any{}
			for k, v := range m {
				entry[k] = v
			}
			var ref *schemaReasonRef
			if s, isString := m["migration"].(string); isString {
				if n, stored := related[s]; stored && n.Kind == "migration" {
					ref = &schemaReasonRef{ID: n.Identity, File: n.File, Version: n.Attributes["version"], Order: n.Attributes["order"]}
				}
			}
			entry["migration_ref"] = ref
			entry["link"] = p.reasonLink(m, ref)
			out.Reasons = append(out.Reasons, entry)
		}
		trunc := &schemaTruncated{Reasons: []string{}}
		for _, dir := range []struct {
			pairs map[string][]neighbourPair
			dst   *[]schemaNeighbour
		}{{outPairs, &out.Neighbours.Outgoing}, {inPairs, &out.Neighbours.Incoming}} {
			kept, tables, pairs := groupNeighbours(dir.pairs, NeighboursCap)
			trunc.TablesOmitted += tables
			trunc.PairsOmitted += pairs
			for _, k := range kept {
				n := neighbourNodes[k.table]
				*dir.dst = append(*dir.dst, schemaNeighbour{Table: servedTable(n), Completeness: tableCompleteness(n), Pairs: k.pairs})
			}
		}
		if trunc.TablesOmitted > 0 {
			trunc.Reasons = append(trunc.Reasons, "neighbours_cap")
			out.Truncated = trunc
		}
		out.RestsOn = tableRestsOn(table, append(append([]contract.Node(nil), columns...), indexes...))
		run, _ := precisionRun(runs)
		out.Precision = labelOf(run, schemaMarkers(allTables, allMigrations)(table))
		if err := tableExtras(rd, p, &out, table, columns, allTables, allMigrations, incoming, related, unread, modelsComplete); err != nil {
			return nil, err
		}
		if out.ModelRoutes, err = tableModelRoutes(rd, p, table); err != nil {
			return nil, err
		}
		return out, nil
	}, nil
}

// objectsOrRaw returns the elements of an array attribute (any JSON type),
// or none when the value is not an array.
func objectsOrRaw(v any) []any {
	items, _ := v.([]any)
	return items
}

func evidenceOf(e contract.Edge) []contract.Evidence {
	if e.Evidence == nil {
		return []contract.Evidence{}
	}
	return e.Evidence
}

func migrationRef(n contract.Node) schemaMigrationRef {
	return schemaMigrationRef{ID: n.Identity, File: n.File, Version: n.Attributes["version"], Order: n.Attributes["order"], Description: n.Attributes["description"]}
}

// tableHistory is the created event (the first creating migration by
// identity, with the table's own span) followed by one altered event per
// incoming alters edge (with the edge's evidence), ordered by the
// migrations' integer order, then identity.
func tableHistory(table contract.Node, incoming []contract.Edge, related map[string]contract.Node) []schemaEvent {
	out := []schemaEvent{}
	var creator *contract.Node
	var altered []contract.Node
	evidence := map[string][]contract.Evidence{}
	for _, e := range incoming {
		m, stored := related[e.Source]
		if !stored || m.Kind != "migration" {
			continue
		}
		switch e.Kind {
		case "contains":
			if creator == nil || m.Identity < creator.Identity {
				m := m
				creator = &m
			}
		case "alters":
			altered = append(altered, m)
			evidence[m.Identity] = evidenceOf(e)
		}
	}
	if creator != nil {
		ev := []contract.Evidence{}
		if table.Span != nil {
			ev = append(ev, contract.Evidence{File: table.File, Span: *table.Span})
		}
		out = append(out, schemaEvent{Migration: migrationRef(*creator), Event: "created", Evidence: ev})
	}
	sort.Slice(altered, func(i, j int) bool {
		oi, okI := intValue(altered[i].Attributes["order"])
		oj, okJ := intValue(altered[j].Attributes["order"])
		if okI != okJ {
			return okI
		}
		if okI && oi != oj {
			return oi < oj
		}
		return altered[i].Identity < altered[j].Identity
	})
	for _, m := range altered {
		out = append(out, schemaEvent{Migration: migrationRef(m), Event: "altered", Evidence: evidence[m.Identity]})
	}
	return out
}

// GET /api/v0/schema/migration

type schemaMigrationResponse struct {
	APIVersion       int                   `json:"api_version"`
	Repository       string                `json:"repository"`
	Index            *lastRun              `json:"index"`
	IDE              ideStatus             `json:"ide"`
	Set              *string               `json:"set"`
	Migration        presentedNode         `json:"migration"`
	Previous         *string               `json:"previous"`
	Next             *string               `json:"next"`
	Creates          []schemaTableRef      `json:"creates"`
	Alters           []schemaAlterRef      `json:"alters"`
	Diagnostics      []schemaFileDiagnosis `json:"diagnostics"`
	DiagnosticsTotal int                   `json:"diagnostics_total"`
	// OpaqueSites is the stored list with an IDE link per statement
	// (T018f); null when the record has none.
	OpaqueSites []map[string]any `json:"opaque_sites"`
}

type schemaTableRef struct {
	ID           string `json:"id"`
	Name         any    `json:"name"`
	Completeness string `json:"completeness"`
}

// schemaAlterRef is an altered table with the alters edge's evidence.
type schemaAlterRef struct {
	schemaTableRef
	Evidence []contract.Evidence `json:"evidence"`
}

// schemaFileDiagnosis is a stored diagnostic without its file, which is the
// migration's.
type schemaFileDiagnosis struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

func computeSchemaMigration(h *apiHandler, q url.Values) (readFunc, error) {
	id, err := schemaID(q, "migration")
	if err != nil {
		return nil, err
	}
	editor, err := editorParam(q)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, rd *reader, runs []store.Run) (any, error) {
		found, err := rd.byIdentity([]string{id})
		if err != nil {
			return nil, err
		}
		m, ok := found[id]
		if !ok || m.Kind != "migration" {
			return nil, notFound("migration", id)
		}
		edges, err := rd.edgeKinds([]string{id}, []string{"contains", "alters"}, store.Outgoing)
		if err != nil {
			return nil, err
		}
		targets := []string{}
		for _, e := range edges {
			targets = append(targets, e.Target)
		}
		tables, err := rd.byIdentity(targets)
		if err != nil {
			return nil, err
		}
		all, err := rd.nodes("migration")
		if err != nil {
			return nil, err
		}
		diags, err := rd.diagnostics([]string{m.File})
		if err != nil {
			return nil, err
		}
		hashes, err := rd.fileHashes()
		if err != nil {
			return nil, err
		}
		p := h.newPresenter(hashes, editor)
		set := stringValue(m.Attributes["set"])
		out := schemaMigrationResponse{
			Repository: h.opt.Repository, Index: indexOf(runs), IDE: p.ide, Set: set, Migration: p.node(m),
			Creates: []schemaTableRef{}, Alters: []schemaAlterRef{}, Diagnostics: []schemaFileDiagnosis{}, DiagnosticsTotal: len(diags),
		}
		out.Previous, out.Next = neighbourMigrations(m, all)
		for _, e := range edges {
			t, stored := tables[e.Target]
			if !stored || t.Kind != "table" {
				continue
			}
			ref := schemaTableRef{ID: t.Identity, Name: t.Attributes["name"], Completeness: completeness(t.Attributes)}
			if e.Kind == "alters" {
				out.Alters = append(out.Alters, schemaAlterRef{schemaTableRef: ref, Evidence: evidenceOf(e)})
			} else {
				out.Creates = append(out.Creates, ref)
			}
		}
		if sites, isList := m.Attributes["opaque_sites"].([]any); isList {
			ref := &schemaReasonRef{ID: m.Identity, File: m.File}
			out.OpaqueSites = []map[string]any{}
			for _, raw := range sites {
				entry := map[string]any{}
				if site, isObject := raw.(map[string]any); isObject {
					for k, v := range site {
						entry[k] = v
					}
				}
				entry["link"] = p.statementLink(ref, entry["span"])
				out.OpaqueSites = append(out.OpaqueSites, entry)
			}
		}
		for _, d := range diags {
			if len(out.Diagnostics) == migrationDiagnosticsCap {
				break
			}
			out.Diagnostics = append(out.Diagnostics, schemaFileDiagnosis{Severity: d.Severity, Code: d.Code, Message: d.Message})
		}
		return out, nil
	}, nil
}

// neighbourMigrations are the migrations of the same set right before and
// after m by stored integer order (whatever their applied); null at the
// ends or when m has no integer order or no string set.
func neighbourMigrations(m contract.Node, all []contract.Node) (previous, next *string) {
	set := stringValue(m.Attributes["set"])
	order, ok := intValue(m.Attributes["order"])
	if set == nil || !ok {
		return nil, nil
	}
	var prev, nxt *contract.Node
	var prevOrder, nextOrder int64
	for i := range all {
		n := all[i]
		if n.Identity == m.Identity {
			continue
		}
		s := stringValue(n.Attributes["set"])
		o, isInt := intValue(n.Attributes["order"])
		if s == nil || *s != *set || !isInt {
			continue
		}
		if o < order && (prev == nil || o > prevOrder || o == prevOrder && n.Identity > prev.Identity) {
			prev, prevOrder = &all[i], o
		}
		if o > order && (nxt == nil || o < nextOrder || o == nextOrder && n.Identity < nxt.Identity) {
			nxt, nextOrder = &all[i], o
		}
	}
	if prev != nil {
		previous = &prev.Identity
	}
	if nxt != nil {
		next = &nxt.Identity
	}
	return previous, next
}

// Reads the schema endpoints add to the request reader.

// edgeKinds reads the edges of the given kinds touching ids.
func (rd *reader) edgeKinds(ids, kinds []string, dir store.Direction) ([]contract.Edge, error) {
	if len(ids) == 0 {
		return []contract.Edge{}, nil
	}
	edges, err := rd.s.Edges(rd.ctx, rd.repo, ids, kinds, dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s %v edges: %w", dir, kinds, err)
	}
	return edges, nil
}

// diagnostics reads the stored diagnostics of files, in stored order.
func (rd *reader) diagnostics(files []string) ([]contract.Diagnostic, error) {
	if len(files) == 0 {
		return []contract.Diagnostic{}, nil
	}
	out, err := rd.s.Diagnostics(rd.ctx, rd.repo, files)
	if err != nil {
		return nil, fmt.Errorf("reading diagnostics: %w", err)
	}
	return out, nil
}

// unfiledDiagnostics reads the diagnostics whose file is not stored, where
// the scanner reports what it did not read.
func (rd *reader) unfiledDiagnostics() ([]contract.Diagnostic, error) {
	out, err := rd.s.DiagnosticsWithoutFile(rd.ctx, rd.repo)
	if err != nil {
		return nil, fmt.Errorf("reading diagnostics without a file: %w", err)
	}
	return out, nil
}

// manifestsOf lists the stored package manifests (Cargo.toml) among the
// stored file paths (hashes' keys); they carry the diagnostics of failed
// sets, sets without records and unreadable manifests. It takes the file
// list the request already read, so one request reads it once.
func manifestsOf(hashes map[string]string) []string {
	out := []string{}
	for file := range hashes {
		if isManifest(file) {
			out = append(out, file)
		}
	}
	sort.Strings(out)
	return out
}

// manifestDiagnostics reads the diagnostics of the stored manifests for
// the summary.
func (rd *reader) manifestDiagnostics(hashes map[string]string) ([]string, []contract.Diagnostic, error) {
	manifests := manifestsOf(hashes)
	diags, err := rd.diagnostics(manifests)
	if err != nil {
		return nil, nil, err
	}
	return manifests, diags, nil
}

// unreadTools applies the marker rule to the stored file paths and the
// Prisma, Drizzle, Sequelize and Diesel linkers' schema_model_migrations
// markers, the GORM linker's GORM v1 markers (gormV1Files) and the TypeORM
// linker's (withTypeORM); nil (null) when not indexed.
func (rd *reader) unreadTools(hashes map[string]string, last *lastRun) ([]schemaUnreadTool, error) {
	if last == nil {
		return nil, nil
	}
	// Only the paths the marker rule reads are kept before sorting: sorting
	// every stored path cost several ms per request on langflow (F7), and
	// the rule's result does not depend on the other paths.
	marks, err := rd.s.DiagnosticsWithCodes(rd.ctx, rd.repo, []string{codePrismaMigrations}, nil)
	if err != nil {
		return nil, fmt.Errorf("reading migrations markers: %w", err)
	}
	// Of the schema_model_unread diagnostics (every model linker's row D
	// markers) only the GORM v1 markers count here (T018g5 Revision 2, C3).
	v1, err := rd.s.DiagnosticsWithMessage(rd.ctx, rd.repo, codeModelUnread, gormV1MarkerText)
	if err != nil {
		return nil, fmt.Errorf("reading GORM v1 markers: %w", err)
	}
	drizzle := namedMigrations(marks, hashes, drizzleMigrationsMarker)
	sequelize := namedMigrations(marks, hashes, sequelizeMigrationsMarker)
	typeorm := namedMigrations(marks, hashes, typeormMigrationsMarker)
	diesel := namedMigrations(marks, hashes, dieselMigrationsMarker)
	files := []string{}
	var typeormFiles []string
	for f := range hashes {
		if alembicPath(f) || len(drizzle) > 0 && drizzlePath(f, drizzle) || sequelizeDir(f, sequelize) != "" || dieselDir(f, diesel) != "" {
			files = append(files, f)
		}
		if sequelizeDir(f, typeorm) != "" {
			typeormFiles = append(typeormFiles, f)
		}
	}
	sort.Strings(files)
	return withGormV1(withTypeORM(unreadTools(files, namedMigrations(marks, hashes, prismaMigrationsMarker), drizzle, sequelize, diesel), typeormFiles, typeorm), gormV1Files(v1, hashes)), nil
}

// hasModelTables reports a stored model table.
func hasModelTables(tables []contract.Node) bool {
	for _, t := range tables {
		if modelRecord(t) {
			return true
		}
	}
	return false
}

// modelDiagnostics reads, in stored order, the diagnostics of the stored
// files (hashes' keys) that the model rules read: the schema_ codes of any
// stored file (the model linkers' per-file counts, markers and failures:
// Python, and from T018g2 review the TypeScript and Prisma linkers',
// which a non-SQLModel group reads) and, for .py files, the codes that say
// a file was not read (fileNotReadCodes). The other diagnostics (unresolved
// calls, routes, ...) change no model rule, and decoding them cost most of
// /schema/sets on langflow (F7), so the store filters by code and this
// filters by file.
func (rd *reader) modelDiagnostics(hashes map[string]string) ([]contract.Diagnostic, error) {
	codes := sortedKeys(fileNotReadCodes)
	all, err := rd.s.DiagnosticsWithCodes(rd.ctx, rd.repo, codes, []string{"schema_"})
	if err != nil {
		return nil, fmt.Errorf("reading model diagnostics: %w", err)
	}
	out := []contract.Diagnostic{}
	for _, d := range all {
		if _, stored := hashes[d.File]; stored && (strings.HasSuffix(d.File, ".py") || strings.HasPrefix(d.Code, "schema_")) {
			out = append(out, d)
		}
	}
	return out, nil
}

// spanOf reads a stored span object; ok is false unless every position is
// an integer.
func spanOf(v any) (contract.Span, bool) {
	m, isObject := v.(map[string]any)
	if !isObject {
		return contract.Span{}, false
	}
	var out contract.Span
	for _, f := range []struct {
		key string
		dst *int
	}{{"start_line", &out.StartLine}, {"start_column", &out.StartColumn}, {"end_line", &out.EndLine}, {"end_column", &out.EndColumn}} {
		n, ok := intValue(m[f.key])
		if !ok {
			return contract.Span{}, false
		}
		*f.dst = int(n)
	}
	return out, true
}

// link is the IDE link to a position in file under the request's editor,
// built exactly as a node's ide link; nil when links are unavailable.
func (p *presenter) link(file string, span contract.Span) *query.IDELink {
	if !p.ide.Available || file == "" || file == "." {
		return nil
	}
	return p.p.Node(contract.Node{File: file, Span: &span}, p.hashes[file]).IDE
}

// statementLink links the start of a stored statement span in migration.
func (p *presenter) statementLink(migration *schemaReasonRef, span any) *query.IDELink {
	sp, ok := spanOf(span)
	if migration == nil || !ok {
		return nil
	}
	return p.link(migration.File, sp)
}

// reasonLink links a reason's line: a migration reason's statement span,
// or a model reason's file and line.
func (p *presenter) reasonLink(m map[string]any, ref *schemaReasonRef) *query.IDELink {
	if ref != nil {
		return p.statementLink(ref, m["span"])
	}
	file, isFile := m["file"].(string)
	line, isLine := intValue(m["line"])
	if !isFile || !isLine || line < 1 {
		return nil
	}
	return p.link(file, contract.Span{StartLine: int(line), EndLine: int(line)})
}

// migrationRefOf names a stored migration for a reason, claim or site.
func migrationRefOf(id any, related map[string]contract.Node) *schemaReasonRef {
	s, isString := id.(string)
	if !isString {
		return nil
	}
	n, stored := related[s]
	if !stored || n.Kind != "migration" {
		return nil
	}
	return &schemaReasonRef{ID: n.Identity, File: n.File, Version: n.Attributes["version"], Order: n.Attributes["order"]}
}

// claims presents a record's stored claims with migration_ref and link;
// nil when the record has no claims list. A model claim (T018g0) names its
// file instead of a migration: its link is the file's span, and its
// migration_ref is null.
func (p *presenter) claims(n contract.Node, related map[string]contract.Node) []map[string]any {
	list, isList := n.Attributes["claims"].([]any)
	if !isList {
		return nil
	}
	out := []map[string]any{}
	for _, raw := range list {
		entry := map[string]any{}
		if c, isObject := raw.(map[string]any); isObject {
			for k, v := range c {
				entry[k] = v
			}
		}
		ref := migrationRefOf(entry["migration"], related)
		entry["migration_ref"] = ref
		entry["link"] = p.statementLink(ref, entry["span"])
		if file, isFile := entry["file"].(string); isFile && ref == nil {
			if sp, ok := spanOf(entry["span"]); ok {
				entry["link"] = p.link(file, sp)
			}
		}
		out = append(out, entry)
	}
	return out
}

// tableExtras adds T018f's opaque sites and overlaps to a table response.
func tableExtras(rd *reader, p *presenter, out *schemaTableResponse, table contract.Node, columns, allTables, allMigrations []contract.Node, incoming []contract.Edge, related map[string]contract.Node, unread []schemaUnreadTool, modelsComplete bool) error {
	if !modelRecord(table) {
		var creator *string
		for _, e := range incoming {
			if m, stored := related[e.Source]; e.Kind == "contains" && stored && m.Kind == "migration" && (creator == nil || e.Source < *creator) {
				id := e.Source
				creator = &id
			}
		}
		set := groupKey(table)
		var group []contract.Node
		for _, m := range allMigrations {
			if k := groupKey(m); set != nil && k != nil && *k == *set {
				group = append(group, m)
			}
		}
		sort.Slice(group, func(i, j int) bool { return lessByOrder(group[i], group[j]) })
		if sites := tableOpaqueSites(table, creator, group); sites != nil {
			out.OpaqueSites = []schemaLinkedSite{}
			for _, site := range sites {
				ref := migrationRefOf(site.Migration, related)
				out.OpaqueSites = append(out.OpaqueSites, schemaLinkedSite{schemaOpaqueSite: site, MigrationRef: ref, Link: p.statementLink(ref, site.Span)})
			}
		}
	}
	name, _ := table.Attributes["name"].(string)
	var pairs []contract.Node
	for _, t := range allTables {
		if other, _ := t.Attributes["name"].(string); other == name && modelRecord(t) != modelRecord(table) {
			pairs = append(pairs, t)
		}
	}
	names := map[string][]string{}
	for _, c := range columns {
		if n, ok := c.Attributes["name"].(string); ok {
			names[table.Identity] = append(names[table.Identity], n)
		}
	}
	if len(pairs) > 0 {
		edges, err := rd.edgeKinds(identities(pairs), []string{"contains"}, store.Outgoing)
		if err != nil {
			return err
		}
		targets := []string{}
		for _, e := range edges {
			targets = append(targets, e.Target)
		}
		cols, err := rd.byIdentity(targets)
		if err != nil {
			return err
		}
		for _, e := range edges {
			if c, stored := cols[e.Target]; stored && c.Kind == "column" {
				if n, ok := c.Attributes["name"].(string); ok {
					names[e.Source] = append(names[e.Source], n)
				}
			}
		}
	}
	byID := map[string]contract.Node{}
	for _, t := range pairs {
		byID[t.Identity] = t
	}
	if !overlapsProven(table, unread, modelsComplete) {
		return nil
	}
	out.Overlaps = []schemaTableOverlap{}
	for _, o := range overlapsOf(table, pairs, func(id string) []string { return names[id] }) {
		other := byID[o.Table]
		out.Overlaps = append(out.Overlaps, schemaTableOverlap{schemaOverlap: o, Completeness: tableCompleteness(other), Name: other.Attributes["name"]})
	}
	return nil
}

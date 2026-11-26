package server

import (
	"encoding/json"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/precision"
)

// This file holds the pure rules of the schema endpoints (ADR 0048,
// docs/cli/serve.md): grouping and ordering of the stored schema records
// and every field the API derives from them. Each derived field has one
// rule here, and each rule's fallback is unknown: a missing attribute or one
// of an unexpected JSON type never becomes false, 0, none or complete.

// firstSchemaVersion is the first aggregate extractor version
// (extractors-N) known to include the schema pass: T018a merged at
// extractors-27 without a bump, so 27 is used by builds before and after
// it, and T018b1 bumped to 28 so that an index can prove the pass ran.
const firstSchemaVersion = 28

// modeledDialects are the dialects whose tables T018a extracts (ADR 0047:
// Postgres only; other sets get migration records alone). It must follow
// the extractor's dialect support.
var modeledDialects = map[string]bool{"postgres": true}

// Table completeness values.
const (
	completenessComplete = "complete"
	completenessInSource = "complete_in_source"
	completenessPartial  = "partial"
	completenessUnknown  = "unknown"
)

// Column default states.
const (
	defaultWritten = "written"
	defaultImplied = "implied"
	defaultNone    = "none"
	defaultUnknown = "unknown"
)

// Set-level values.
const (
	objectsModeled      = "modeled"
	objectsNotModeled   = "not_modeled"
	tableListComplete   = "complete"
	tableListMaybe      = "may_be_incomplete"
	tableListNotModeled = "not_modeled"
)

// Summary found values.
const (
	foundSets    = "sets"
	foundNone    = "none"
	foundUnknown = "unknown"
)

// NeighboursCap bounds the neighbour tables per direction of /schema/table
// (truncated); the MCP table tool's bound reads it (T030 R10).
const NeighboursCap = 200

// migrationDiagnosticsCap bounds the diagnostics of /schema/migration.
const migrationDiagnosticsCap = 500

// intValue returns an attribute value that is a JSON integer. Stored
// payloads are decoded with UseNumber; plain Go integers are accepted for
// values built in code. Anything else, including a string of digits, is not
// an integer.
func intValue(v any) (int64, bool) {
	switch n := v.(type) {
	case json.Number:
		i, err := strconv.ParseInt(string(n), 10, 64)
		return i, err == nil
	case int:
		return int64(n), true
	case int64:
		return n, true
	}
	return 0, false
}

// stringValue returns an attribute value that is a JSON string.
func stringValue(v any) *string {
	if s, ok := v.(string); ok {
		return &s
	}
	return nil
}

// completeness is the one rule for a table's state: complete only for state
// "complete" with the integer opaque_after 0; complete_in_source for state
// "complete" with an integer above 0 (complete as far as the migrations
// show); partial for state "partial"; unknown for anything else.
func completeness(attrs map[string]any) string {
	state, _ := attrs["state"].(string)
	switch state {
	case "complete":
		n, ok := intValue(attrs["opaque_after"])
		switch {
		case ok && n == 0:
			return completenessComplete
		case ok && n > 0:
			return completenessInSource
		}
	case "partial":
		return completenessPartial
	}
	return completenessUnknown
}

// defaultState is the one rule for a column's default: written for a
// string default; implied for a null default with a string default_implied;
// none for a null default without default_implied on a complete or
// complete_in_source table; unknown otherwise, so none is never claimed for
// a partial table or a column whose default is missing.
func defaultState(attrs map[string]any, tableCompleteness string) string {
	def, present := attrs["default"]
	if !present {
		return defaultUnknown
	}
	if _, ok := def.(string); ok {
		return defaultWritten
	}
	if def != nil {
		return defaultUnknown
	}
	implied, hasImplied := attrs["default_implied"]
	if hasImplied {
		if _, ok := implied.(string); ok {
			return defaultImplied
		}
		return defaultUnknown
	}
	if tableCompleteness == completenessComplete || tableCompleteness == completenessInSource {
		return defaultNone
	}
	return defaultUnknown
}

// referenceEntries are a column's reference entries: its references value
// (when not null) and each element of its additional_references array. A
// value of another JSON type counts as one entry, so it is reported rather
// than dropped.
func referenceEntries(attrs map[string]any) []any {
	var out []any
	if v, ok := attrs["references"]; ok && v != nil {
		out = append(out, v)
	}
	if v, ok := attrs["additional_references"]; ok && v != nil {
		if items, isList := v.([]any); isList {
			out = append(out, items...)
		} else {
			out = append(out, v)
		}
	}
	return out
}

// linkedEntry is true only for an object entry whose status is exactly
// "linked"; every other entry is unresolved.
func linkedEntry(entry any) bool {
	m, ok := entry.(map[string]any)
	return ok && m["status"] == "linked"
}

// setOpaqueRecord returns the set record (the set_opaque_statements
// attribute T018a writes with the same value on every migration of an
// analysed Postgres set) when it is known: every migration carries an
// object with an integer total and an object by_kind, and all are equal as
// JSON. ok is false for no migrations, a null or malformed record, or
// records that differ.
func setOpaqueRecord(migrations []contract.Node) (record map[string]any, ok bool) {
	var first []byte
	for _, m := range migrations {
		rec, isObject := m.Attributes["set_opaque_statements"].(map[string]any)
		if !isObject {
			return nil, false
		}
		if _, isInt := intValue(rec["total"]); !isInt {
			return nil, false
		}
		if _, isObject := rec["by_kind"].(map[string]any); !isObject {
			return nil, false
		}
		data, err := json.Marshal(rec)
		if err != nil {
			return nil, false
		}
		if first == nil {
			first, record = data, rec
		} else if string(data) != string(first) {
			return nil, false
		}
	}
	return record, record != nil
}

// commonString is the value of key shared by every node: the string when
// all carry the same string, nil without conflict when none carries a
// string, and nil with conflict when they differ.
func commonString(nodes []contract.Node, key string) (value *string, conflict bool) {
	seen := map[string]bool{}
	missing := false
	for _, n := range nodes {
		if s, ok := n.Attributes[key].(string); ok {
			seen[s] = true
		} else {
			missing = true
		}
	}
	if len(seen) == 0 {
		return nil, false
	}
	if len(seen) > 1 || missing {
		return nil, true
	}
	for s := range seen {
		return &s, false
	}
	return nil, false
}

// testOnly is true when every migration has scope exactly "test", false
// when some or none have it, and null without migrations.
func testOnly(migrations []contract.Node) *bool {
	if len(migrations) == 0 {
		return nil
	}
	all := true
	for _, m := range migrations {
		if m.Attributes["scope"] != "test" {
			all = false
		}
	}
	return &all
}

// setFiles are the files whose diagnostics belong to a set: its migration
// files, each package manifest its migrations name (all of them, so a
// package conflict cannot hide a manifest's diagnostics), and sqlx.toml in
// each package directory, where T018a writes schema_unsupported_config.
func setFiles(migrations []contract.Node) []string {
	out := []string{}
	for _, m := range migrations {
		out = append(out, m.File)
		if pkg, ok := m.Attributes["package"].(string); ok {
			out = append(out, pkg, sqlxConfig(pkg))
		}
	}
	return distinct(out)
}

// setState is the derived state of one set.
type setState struct {
	objects           string
	notModeledReasons []string
	tableList         string
	tableListReasons  []string
	opaqueTotal       any
	opaqueByKind      any
}

// deriveSetState applies the objects and table_list rules. dialect is the
// common dialect (nil when absent or in conflict); codes are the
// diagnostic codes stored for the set's files.
func deriveSetState(dialect *string, migrations []contract.Node, codes map[string]int) setState {
	out := setState{notModeledReasons: []string{}, tableListReasons: []string{}}
	record, known := setOpaqueRecord(migrations)
	if dialect == nil || !modeledDialects[*dialect] {
		out.notModeledReasons = append(out.notModeledReasons, "dialect_unsupported")
	}
	if codes["schema_migration_order"] > 0 {
		out.notModeledReasons = append(out.notModeledReasons, "order_failed")
	}
	if len(out.notModeledReasons) == 0 && !known {
		out.notModeledReasons = append(out.notModeledReasons, "not_analysed")
	}
	sort.Strings(out.notModeledReasons)
	if len(out.notModeledReasons) > 0 {
		out.objects, out.tableList = objectsNotModeled, tableListNotModeled
		return out
	}
	out.objects = objectsModeled
	out.opaqueTotal, out.opaqueByKind = record["total"], record["by_kind"]
	if total, _ := intValue(record["total"]); total > 0 {
		out.tableListReasons = append(out.tableListReasons, "opaque_statements")
	}
	if codes["schema_member_unreadable"] > 0 {
		out.tableListReasons = append(out.tableListReasons, "member_unreadable")
	}
	if codes["schema_unsupported_config"] > 0 {
		out.tableListReasons = append(out.tableListReasons, "unsupported_config")
	}
	unsupported, unknownCounts := unsupportedStatements(migrations)
	if unsupported || codes["schema_unsupported_statement"] > 0 {
		out.tableListReasons = append(out.tableListReasons, "unsupported_statements")
	}
	if unknownCounts {
		out.tableListReasons = append(out.tableListReasons, "statement_counts_unknown")
	}
	sort.Strings(out.tableListReasons)
	out.tableList = tableListComplete
	if len(out.tableListReasons) > 0 {
		out.tableList = tableListMaybe
	}
	return out
}

// unsupportedStatements reads the per-migration unsupported counts (T018b1
// review). T018a classes the statements that create a table it does not
// model as unsupported, not opaque (SELECT ... INTO t, CREATE TABLE t AS,
// CREATE SCHEMA s CREATE TABLE ...), so they are not in the set record's
// opaque total. unsupported is true when a migration has an integer count
// above 0. unknown is true when a migration whose applied is not exactly
// false has no integer count of 0 or more: T018a leaves the counts null
// only on migrations it does not analyse, which in a modeled set are the
// down migrations (applied false), which sqlx never applies.
func unsupportedStatements(migrations []contract.Node) (unsupported, unknown bool) {
	for _, m := range migrations {
		n, ok := intValue(m.Attributes["unsupported"])
		switch {
		case ok && n > 0:
			unsupported = true
		case ok && n == 0:
		case m.Attributes["applied"] == false:
		default:
			unknown = true
		}
	}
	return unsupported, unknown
}

// extractorsVersion matches graphvein's aggregate version text: a decimal
// without leading zeros.
var extractorsVersion = regexp.MustCompile(`^extractors-(0|[1-9][0-9]*)$`)

// foundState is the one rule for the summary's found field. Sets are found
// when any set is stored in any form (sets is the sum of record sets,
// failed sets and sets without records). none must be proven (T018b1
// review): it needs a graphvein run whose version proves the schema
// pass ran, no scan gap (scanGaps: a directory, symlink or Cargo.toml the
// scanner did not read) and no manifest the pass could not read
// (unreadableManifests); everything else is unknown with its reason.
func foundState(last *lastRun, sets, unreadableManifests, scanGaps int) (found string, reason *string) {
	unknown := func(r string) (string, *string) { return foundUnknown, &r }
	if sets > 0 {
		return foundSets, nil
	}
	if last == nil {
		return unknown("not_indexed")
	}
	if last.Indexer.Name != "graphvein" {
		return unknown("external_indexer")
	}
	m := extractorsVersion.FindStringSubmatch(last.Indexer.Version)
	if m == nil {
		return unknown("version_unreadable")
	}
	n, err := strconv.Atoi(m[1])
	switch {
	case err != nil:
		return unknown("version_unreadable")
	case n == firstSchemaVersion-1:
		return unknown("version_ambiguous")
	case n < firstSchemaVersion:
		return unknown("predates_schema")
	case scanGaps > 0:
		return unknown("scan_incomplete")
	case unreadableManifests > 0:
		return unknown("manifest_unreadable")
	}
	return foundNone, nil
}

// lessByOrder orders migrations by integer order, then file and identity;
// migrations without an integer order come last.
func lessByOrder(a, b contract.Node) bool {
	oa, okA := intValue(a.Attributes["order"])
	ob, okB := intValue(b.Attributes["order"])
	if okA != okB {
		return okA
	}
	if okA && oa != ob {
		return oa < ob
	}
	if a.File != b.File {
		return a.File < b.File
	}
	return a.Identity < b.Identity
}

// lessByPosition orders columns by integer position, then identity; columns
// without an integer position come last.
func lessByPosition(a, b contract.Node) bool {
	pa, okA := intValue(a.Attributes["position"])
	pb, okB := intValue(b.Attributes["position"])
	if okA != okB {
		return okA
	}
	if okA && pa != pb {
		return pa < pb
	}
	return a.Identity < b.Identity
}

// lessByQualifiedName orders tables and indexes by qualified name, then
// identity.
func lessByQualifiedName(a, b contract.Node) bool {
	if a.QualifiedName != b.QualifiedName {
		return a.QualifiedName < b.QualifiedName
	}
	return a.Identity < b.Identity
}

// lessSetKey orders set keys in byte order, null last.
func lessSetKey(a, b *string) bool {
	if a == nil || b == nil {
		return a != nil && b == nil
	}
	return *a < *b
}

// neighbourPair is one foreign-key column pair between two tables.
type neighbourPair struct {
	SourceColumn string `json:"source_column"`
	TargetColumn string `json:"target_column"`
}

// neighbourGroup collects the pairs that connect one neighbour table.
type neighbourGroup struct {
	table string
	pairs []neighbourPair
}

// groupNeighbours groups pairs by neighbour table (ordered by identity,
// pairs ordered by source then target column) and cuts the list at limit.
// It returns the kept groups and the omitted table and pair counts.
func groupNeighbours(pairs map[string][]neighbourPair, limit int) (kept []neighbourGroup, tablesOmitted, pairsOmitted int) {
	ids := make([]string, 0, len(pairs))
	for id := range pairs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for i, id := range ids {
		list := pairs[id]
		if i >= limit {
			tablesOmitted++
			pairsOmitted += len(list)
			continue
		}
		sort.Slice(list, func(a, b int) bool {
			if list[a].SourceColumn != list[b].SourceColumn {
				return list[a].SourceColumn < list[b].SourceColumn
			}
			return list[a].TargetColumn < list[b].TargetColumn
		})
		kept = append(kept, neighbourGroup{table: id, pairs: list})
	}
	return kept, tablesOmitted, pairsOmitted
}

// schemaGraph is what /schema/sets reads: the stored schema nodes, their
// edges and the diagnostics of the sets' files.
type schemaGraph struct {
	migrations, tables, columns, indexes []contract.Node
	// migrationEdges are the outgoing contains and alters edges of the
	// migrations; tableEdges the outgoing contains edges of the tables;
	// foreignKeys the outgoing foreign_key edges of the columns.
	migrationEdges, tableEdges, foreignKeys []contract.Edge
	diagnostics                             []contract.Diagnostic
	// modelDiagnostics are the stored diagnostics of the Python files
	// (T018f: model groups), and read the index's sources_read.
	modelDiagnostics []contract.Diagnostic
	read             []string
	// unfiled are the diagnostics without a stored file (scanner gaps);
	// unreadTools the migration tools found but not read (1.3).
	unfiled     []contract.Diagnostic
	unreadTools []schemaUnreadTool
	// run labels each group's scope (precision.go).
	run precision.Run
}

// groupKey returns a node's set key: its string set attribute, or nil.
func groupKey(n contract.Node) *string { return stringValue(n.Attributes["set"]) }

func keyString(k *string) string {
	if k == nil {
		return "\x00null"
	}
	return "s" + *k
}

// groupID is the group of a table: its source and set key, so that a model
// group never merges with a migration set of the same key text (T018f G3).
func groupID(model bool, k *string) string {
	if model {
		return "m" + keyString(k)
	}
	return keyString(k)
}

// buildSets groups the schema records by set (section 2 of the task,
// docs/cli/serve.md). Every link it reports comes from a stored edge.
func buildSets(g schemaGraph) []schemaSet {
	tables := map[string]contract.Node{}
	for _, n := range g.tables {
		tables[n.Identity] = n
	}
	columns := map[string]contract.Node{}
	for _, n := range g.columns {
		columns[n.Identity] = n
	}
	indexes := map[string]contract.Node{}
	for _, n := range g.indexes {
		indexes[n.Identity] = n
	}
	// Containment from stored contains edges only.
	tableColumns, tableIndexes := map[string][]contract.Node{}, map[string][]contract.Node{}
	columnTable := map[string]string{}
	attached := map[string]bool{}
	for _, e := range g.tableEdges {
		if e.Kind != "contains" {
			continue
		}
		if _, ok := tables[e.Source]; !ok {
			continue
		}
		if c, ok := columns[e.Target]; ok {
			tableColumns[e.Source] = append(tableColumns[e.Source], c)
			if cur, seen := columnTable[e.Target]; !seen || e.Source < cur {
				columnTable[e.Target] = e.Source
			}
			attached[e.Target] = true
		}
		if ix, ok := indexes[e.Target]; ok {
			tableIndexes[e.Source] = append(tableIndexes[e.Source], ix)
			attached[e.Target] = true
		}
	}
	creates, alters := map[string][]string{}, map[string][]string{}
	createdBy, alteredBy := map[string]string{}, map[string][]string{}
	for _, e := range g.migrationEdges {
		if _, ok := tables[e.Target]; !ok {
			continue
		}
		switch e.Kind {
		case "contains":
			creates[e.Source] = append(creates[e.Source], e.Target)
			if cur, seen := createdBy[e.Target]; !seen || e.Source < cur {
				createdBy[e.Target] = e.Source
			}
		case "alters":
			alters[e.Source] = append(alters[e.Source], e.Target)
			alteredBy[e.Target] = append(alteredBy[e.Target], e.Source)
		}
	}
	// Foreign-key pairs, with both ends' tables by contains; counts per
	// table are taken over every stored pair.
	type fkPair struct {
		edge                     contract.Edge
		sourceTable, targetTable string
		hasTargetTable           bool
	}
	pairsBySet := map[string][]fkPair{}
	out, in := map[string]int{}, map[string]int{}
	hasEdge := map[string]bool{}
	for _, e := range g.foreignKeys {
		if e.Kind != "foreign_key" {
			continue
		}
		hasEdge[e.Source] = true
		st, ok := columnTable[e.Source]
		if !ok {
			continue
		}
		tt, hasTT := columnTable[e.Target]
		out[st]++
		if hasTT {
			in[tt]++
		}
		key := groupID(modelRecord(tables[st]), groupKey(tables[st]))
		pairsBySet[key] = append(pairsBySet[key], fkPair{edge: e, sourceTable: st, targetTable: tt, hasTargetTable: hasTT})
	}

	type group struct {
		key        *string
		model      bool
		migrations []contract.Node
		tables     []contract.Node
	}
	groups := map[string]*group{}
	groupOf := func(model bool, k *string) *group {
		ks := groupID(model, k)
		if groups[ks] == nil {
			groups[ks] = &group{key: k, model: model}
		}
		return groups[ks]
	}
	for _, m := range g.migrations {
		gr := groupOf(false, groupKey(m))
		gr.migrations = append(gr.migrations, m)
	}
	for _, t := range g.tables {
		gr := groupOf(modelRecord(t) && groupKey(t) != nil, groupKey(t))
		gr.tables = append(gr.tables, t)
	}
	columnNames := func(table string) []string {
		out := []string{}
		for _, c := range tableColumns[table] {
			if name, ok := c.Attributes["name"].(string); ok {
				out = append(out, name)
			}
		}
		return out
	}
	unattachedColumns, unattachedIndexes := 0, 0
	for _, c := range g.columns {
		if !attached[c.Identity] {
			unattachedColumns++
		}
	}
	for _, ix := range g.indexes {
		if !attached[ix.Identity] {
			unattachedIndexes++
		}
	}
	if unattachedColumns+unattachedIndexes > 0 {
		groupOf(false, nil)
	}

	diagnosticsByFile := map[string][]contract.Diagnostic{}
	for _, d := range g.diagnostics {
		diagnosticsByFile[d.File] = append(diagnosticsByFile[d.File], d)
	}

	modelsComplete := modelListsComplete(g.read, modelGroupsOf(g.tables), g.modelDiagnostics, g.unfiled)
	result := []schemaSet{}
	for _, gr := range groups {
		sort.Slice(gr.migrations, func(i, j int) bool { return lessByOrder(gr.migrations[i], gr.migrations[j]) })
		sort.Slice(gr.tables, func(i, j int) bool { return lessByQualifiedName(gr.tables[i], gr.tables[j]) })
		rank := map[string]int{}
		for i, m := range gr.migrations {
			rank[m.Identity] = i
		}
		s := schemaSet{
			Set: gr.key, Conflicts: []string{}, NotModeledReasons: []string{}, TableListReasons: []string{},
			DiagnosticCounts: map[string]int{}, Migrations: []schemaSetMigration{}, Tables: []schemaSetTable{}, ForeignKeys: []schemaForeignKey{},
			TablesByScope: tablesByScope(gr.tables),
		}
		var records []contract.Node
		for _, t := range gr.tables {
			records = append(records, tableColumns[t.Identity]...)
			records = append(records, tableIndexes[t.Identity]...)
		}
		if gr.model {
			modelGroupState(&s, gr.tables, g.modelDiagnostics, g.unfiled, g.read)
			s.RestsOn = groupRestsOn(true, gr.tables, nil, nil)
		} else if gr.key != nil {
			s.RestsOn = groupRestsOn(false, gr.tables, records, gr.migrations)
			if len(gr.migrations) > 0 {
				source := "migrations"
				s.Source = &source
			}
			for _, field := range []struct {
				key string
				dst **string
			}{{"tool", &s.Tool}, {"package", &s.Package}, {"dialect", &s.Dialect}} {
				v, conflict := commonString(gr.migrations, field.key)
				*field.dst = v
				if conflict {
					s.Conflicts = append(s.Conflicts, field.key)
				}
			}
			sort.Strings(s.Conflicts)
			if len(gr.migrations) > 0 {
				s.Platform = gr.migrations[0].Attributes["platform"]
			}
			for _, f := range setFiles(gr.migrations) {
				for _, d := range diagnosticsByFile[f] {
					s.DiagnosticCounts[d.Code]++
				}
			}
			st := deriveSetState(s.Dialect, gr.migrations, s.DiagnosticCounts)
			s.Objects, s.TableList = &st.objects, &st.tableList
			s.NotModeledReasons, s.TableListReasons = st.notModeledReasons, st.tableListReasons
			s.OpaqueTotal, s.OpaqueByKind = st.opaqueTotal, st.opaqueByKind
			s.TestOnly = testOnly(gr.migrations)
		} else {
			s.Unattached = schemaUnattached{Columns: unattachedColumns, Indexes: unattachedIndexes}
		}
		for _, m := range gr.migrations {
			entry := schemaSetMigration{ID: m.Identity, File: m.File, Attributes: m.Attributes, Creates: sortedIDs(creates[m.Identity]), Alters: sortedIDs(alters[m.Identity])}
			s.Migrations = append(s.Migrations, entry)
		}
		for _, t := range gr.tables {
			c := tableCompleteness(t)
			entry := schemaSetTable{Node: servedTable(t), Completeness: c, AlteredBy: []string{}, Columns: []schemaSetColumn{}, Indexes: []contract.Node{}, ForeignKeysOut: out[t.Identity], ForeignKeysIn: in[t.Identity]}
			if m, ok := createdBy[t.Identity]; ok {
				entry.CreatedBy = &m
			}
			entry.RestsOn = tableRestsOn(t, append(append([]contract.Node(nil), tableColumns[t.Identity]...), tableIndexes[t.Identity]...))
			if overlapsProven(t, g.unreadTools, modelsComplete) {
				entry.Overlaps = overlapsOf(t, g.tables, columnNames)
			}
			if !modelRecord(t) {
				entry.OpaqueSites = tableOpaqueSites(t, entry.CreatedBy, gr.migrations)
			}
			by := distinct(alteredBy[t.Identity])
			sort.SliceStable(by, func(i, j int) bool {
				ri, okI := rank[by[i]]
				rj, okJ := rank[by[j]]
				if okI != okJ {
					return okI
				}
				if okI {
					return ri < rj
				}
				return by[i] < by[j]
			})
			entry.AlteredBy = by
			cols := append([]contract.Node(nil), tableColumns[t.Identity]...)
			sort.Slice(cols, func(i, j int) bool { return lessByPosition(cols[i], cols[j]) })
			if modelRecord(t) {
				entry.VerificationCounts = verificationCounts(t)
			}
			for _, col := range cols {
				entry.Columns = append(entry.Columns, schemaSetColumn{Node: setColumnNode(col), DefaultState: columnDefaultState(col, c)})
				linked := false
				for _, r := range columnReferences(col) {
					switch {
					case r.Linked:
						linked = true
					case r.External:
						entry.ExternalReferences++
					default:
						entry.UnresolvedReferences++
					}
				}
				if linked && !hasEdge[col.Identity] {
					s.LinkedWithoutEdge++
				}
			}
			ixs := append([]contract.Node(nil), tableIndexes[t.Identity]...)
			sort.Slice(ixs, func(i, j int) bool { return lessByQualifiedName(ixs[i], ixs[j]) })
			entry.Indexes = append(entry.Indexes, ixs...)
			s.Tables = append(s.Tables, entry)
		}
		s.Precision = labelOf(g.run, schemaGroupMarker(gr.model, gr.key, gr.migrations, gr.tables))
		pairs := pairsBySet[groupID(gr.model, gr.key)]
		sort.Slice(pairs, func(i, j int) bool {
			a, b := pairs[i], pairs[j]
			if a.sourceTable != b.sourceTable {
				return a.sourceTable < b.sourceTable
			}
			if a.edge.Source != b.edge.Source {
				return a.edge.Source < b.edge.Source
			}
			return a.edge.Target < b.edge.Target
		})
		for _, p := range pairs {
			fk := schemaForeignKey{SourceTable: p.sourceTable, SourceColumn: p.edge.Source, TargetColumn: p.edge.Target, Edge: refOf(p.edge)}
			if p.hasTargetTable {
				tt := p.targetTable
				fk.TargetTable = &tt
			}
			s.ForeignKeys = append(s.ForeignKeys, fk)
		}
		result = append(result, s)
	}
	// Migration groups, then model groups (T018f), then the set: null
	// group; by key within each.
	rank := func(s schemaSet) int {
		switch {
		case s.Set == nil:
			return 2
		case s.schemaModelFields != nil:
			return 1
		}
		return 0
	}
	sort.Slice(result, func(i, j int) bool {
		if ri, rj := rank(result[i]), rank(result[j]); ri != rj {
			return ri < rj
		}
		return lessSetKey(result[i].Set, result[j].Set)
	})
	return result
}

// sortedIDs returns the distinct identities in byte order, never nil.
func sortedIDs(ids []string) []string { return distinct(ids) }

// Codes of manifest diagnostics that are not about a set's contents.
const (
	codeInternalError      = "schema_internal_error"
	codeManifestUnreadable = "schema_manifest_unreadable"
)

// isManifest reports a package manifest path (base name Cargo.toml).
func isManifest(file string) bool { return path.Base(file) == "Cargo.toml" }

// sqlxConfig is the sqlx.toml beside a manifest, where T018a writes
// schema_unsupported_config.
func sqlxConfig(manifest string) string {
	if dir := path.Dir(manifest); dir != "." {
		return dir + "/sqlx.toml"
	}
	return "sqlx.toml"
}

// failedSets lists the stored schema_internal_error diagnostics of package
// manifests (Cargo.toml): sets the pass failed on, which have no records.
func failedSets(diagnostics []contract.Diagnostic) []schemaFailedSet {
	out := []schemaFailedSet{}
	for _, d := range diagnostics {
		if d.Code == codeInternalError && isManifest(d.File) {
			out = append(out, schemaFailedSet{Package: d.File, Message: d.Message})
		}
	}
	sortMessages(out, func(f schemaFailedSet) (string, string) { return f.Package, f.Message })
	return out
}

// scanGapScope says which paths of a scanner not-read diagnostic can hide
// a package manifest.
type scanGapScope int

const (
	// gapAnyPath: the path may be a directory or a Cargo.toml, or the
	// diagnostic changes which paths were read.
	gapAnyPath scanGapScope = iota
	// gapManifestFile: the path is a regular file; only a file named
	// Cargo.toml is a manifest.
	gapManifestFile
)

// scanNotRead holds every diagnostic code internal/scan Walk writes, each
// one for something the scanner did not read (T018b1 review;
// TestScanNotReadCodes checks the list against the scanner source):
//
//   - scan_read: a directory could not be listed, so nothing below it is
//     indexed;
//   - symlink: a symlink is not followed; it may point at a directory or
//     be a Cargo.toml itself (the diagnostic does not say which);
//   - invalid_path: an entry with an unsupported path was skipped; it may
//     be a directory, and the diagnostic's file is empty;
//   - ignore_read: an ignore file could not be read, so which rules apply
//     is unknown (a lost negation can exclude a manifest);
//   - file_read: a regular file could not be read; it hides a manifest
//     only when it is named Cargo.toml;
//   - file_binary, file_not_utf8: a regular file was read but holds a NUL
//     byte or is not valid UTF-8, so its content was not indexed; likewise
//     a manifest only when named Cargo.toml;
//   - file_not_regular: a FIFO, socket or device, never a directory; a
//     manifest only by name.
var scanNotRead = map[string]scanGapScope{
	"scan_read":        gapAnyPath,
	"symlink":          gapAnyPath,
	"invalid_path":     gapAnyPath,
	"ignore_read":      gapAnyPath,
	"file_read":        gapManifestFile,
	"file_binary":      gapManifestFile,
	"file_not_utf8":    gapManifestFile,
	"file_not_regular": gapManifestFile,
}

// scanGaps lists the stored scanner diagnostics that may hide a package
// manifest (scanNotRead), in stored order. With one, the index cannot prove
// that no Cargo.toml was missed, so found is never none.
func scanGaps(diagnostics []contract.Diagnostic) []schemaScanGap {
	out := []schemaScanGap{}
	for _, d := range diagnostics {
		scope, ok := scanNotRead[d.Code]
		if !ok || scope == gapManifestFile && !isManifest(d.File) {
			continue
		}
		out = append(out, schemaScanGap{File: d.File, Code: d.Code, Message: d.Message})
	}
	return out
}

// unreadableManifests lists the manifests whose content no pass read: the
// stored schema_manifest_unreadable diagnostics (T018b1 review, the pass
// could not parse the file) and the scanner's symlink diagnostics and
// its regular-file codes (file_read, file_binary, file_not_utf8,
// file_not_regular: gapManifestFile) on a file named Cargo.toml
// (the file was never read). code names the diagnostic the entry comes from. Whether such a
// manifest declares a set is unknown.
func unreadableManifests(diagnostics []contract.Diagnostic) []schemaUnreadableManifest {
	out := []schemaUnreadableManifest{}
	for _, d := range diagnostics {
		switch {
		case !isManifest(d.File):
		case d.Code == codeManifestUnreadable, d.Code == "symlink", scanNotRead[d.Code] == gapManifestFile:
			out = append(out, schemaUnreadableManifest{File: d.File, Code: d.Code, Message: d.Message})
		}
	}
	sortMessages(out, func(f schemaUnreadableManifest) (string, string) { return f.File, f.Message })
	return out
}

// setsWithoutRecords lists the sets T018a found but emitted no record for
// (T018b1 review), for example a set whose every member is unreadable:
// one entry per stored manifest that carries a schema_* diagnostic other
// than schema_internal_error and schema_manifest_unreadable, has no
// schema_internal_error (it is a failed set then) and is named by no stored
// migration's package. T018a writes such diagnostics on a manifest only for
// a set it discovered. diagnostic_counts counts every stored diagnostic of
// the manifest and its sqlx.toml by code.
func setsWithoutRecords(manifests []string, migrations []contract.Node, diagnostics []contract.Diagnostic) []schemaRecordlessSet {
	named := map[string]bool{}
	for _, m := range migrations {
		if pkg, ok := m.Attributes["package"].(string); ok {
			named[pkg] = true
		}
	}
	byFile := map[string][]contract.Diagnostic{}
	for _, d := range diagnostics {
		byFile[d.File] = append(byFile[d.File], d)
	}
	out := []schemaRecordlessSet{}
	for _, file := range distinct(manifests) {
		if named[file] {
			continue
		}
		setCode, failed := false, false
		for _, d := range byFile[file] {
			switch {
			case d.Code == codeInternalError:
				failed = true
			case d.Code == codeManifestUnreadable:
			case strings.HasPrefix(d.Code, "schema_"):
				setCode = true
			}
		}
		if !setCode || failed {
			continue
		}
		counts := map[string]int{}
		for _, f := range []string{file, sqlxConfig(file)} {
			for _, d := range byFile[f] {
				counts[d.Code]++
			}
		}
		out = append(out, schemaRecordlessSet{Package: file, DiagnosticCounts: counts})
	}
	return out
}

// sortMessages orders entries by file, then message.
func sortMessages[T any](list []T, key func(T) (string, string)) {
	sort.SliceStable(list, func(i, j int) bool {
		fi, mi := key(list[i])
		fj, mj := key(list[j])
		if fi != fj {
			return fi < fj
		}
		return mi < mj
	})
}

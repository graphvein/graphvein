package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/graphvein/graphvein/internal/changes"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/precision"
	"github.com/graphvein/graphvein/internal/store"
)

// This file attaches precision labels to change items (T020,
// addition from the mockup round): the item's file label and,
// per changed field, the scope label of that field's fact family. Both are
// computed here, never joined in the UI.

// itemWithPrecision is a change item with its labels.
type itemWithPrecision struct {
	changes.Item
	Precision itemPrecision `json:"precision"`
}

// MarshalJSON writes the item as changes.Item writes it (with its id)
// plus the precision field; the embedded MarshalJSON would drop it.
func (i itemWithPrecision) MarshalJSON() ([]byte, error) {
	item, err := json.Marshal(i.Item)
	if err != nil {
		return nil, err
	}
	labels, err := json.Marshal(i.Precision)
	if err != nil {
		return nil, err
	}
	item = bytes.TrimSpace(item)
	if len(item) < 2 || item[len(item)-1] != '}' {
		return nil, fmt.Errorf("change item encodes as %q, not an object", item)
	}
	out := append([]byte{}, item[:len(item)-1]...)
	if len(item) > 2 {
		out = append(out, ',')
	}
	out = append(out, `"precision":`...)
	out = append(out, labels...)
	return append(out, '}'), nil
}

// itemPrecision holds an item's labels. File is the file label of the
// item's file (head side, else base side) read from the head side's stored
// file record; null fields when the head has no graph to read (a run or
// pin head) or no record of the file. Scope is the label of the item's
// record family, null for a structural record (its file label applies).
// Fields maps each changed field to its family's label: the record's
// scope, except an owner's table_access field, which is a code-to-table
// fact; null where the file label applies. ScopeSide names the
// comparison side whose run decided Scope and Fields: "head", or "base"
// when the item has a base side and the base run is refused, so a refusal
// reason can say which side it is about (reindexing relabels the head,
// never a stored base).
type itemPrecision struct {
	File      fileLabel              `json:"file"`
	Scope     *scopeLabel            `json:"scope"`
	Fields    map[string]*scopeLabel `json:"fields"`
	ScopeSide string                 `json:"scope_side"`
}

// reasonScopeUnknown: the item's family needs a group marker (a schema
// record's tool and dialect) that the head side cannot provide, such as a
// removed record or a head without a graph. Server-made, since no marker
// exists to pass to precision.ScopeLabel; it never yields precise.
const reasonScopeUnknown = "scope_unknown"

// nodeFamilies are the record families decided by kind alone.
var nodeFamilies = map[string]precision.Family{
	"route": familyRoutes, "client_call": familyClientCalls,
	"ipc_command": familyIPCCommands, "ipc_call": familyIPCCalls,
	"server_action": familyServerActions, "action_call": familyActionCalls,
}

var edgeFamilies = map[string]precision.Family{
	"reads_table": precision.FamilyTableAccess, "writes_table": precision.FamilyTableAccess,
	"exposes_route": familyRoutes, "calls_route": familyClientCalls,
	"exposes_command": familyIPCCommands, "invokes_command": familyIPCCalls,
	"exposes_action": familyServerActions, "calls_action": familyActionCalls,
	"calls": familyCodeCalls,
}

// schemaKinds are the node kinds whose family is a schema group's.
var schemaKinds = map[string]bool{"table": true, "column": true, "db_index": true, "migration": true}

// itemsPrecision labels one page of items of a comparison.
func itemsPrecision(ctx context.Context, s store.Store, set store.ChangeSet, items []changes.Item) ([]itemWithPrecision, error) {
	out := make([]itemWithPrecision, 0, len(items))
	if len(items) == 0 {
		return out, nil
	}
	head := runOfIndexer(set.Head.Run.Indexer.Name, set.Head.Run.Indexer.Version)
	base := runOfIndexer(set.Base.Run.Indexer.Name, set.Base.Run.Indexer.Version)
	// Only a current or ref head has a graph of its own to read.
	readable := set.Head.Kind == store.SideCurrent || set.Head.Kind == store.SideRef
	rd := &reader{ctx: ctx, s: s, repo: set.Head.Repository}
	records := map[string]*contract.File{}
	unrooted := map[string]bool{}
	var markerOf func(contract.Node) precision.Marker
	nodes := map[string]contract.Node{}
	tableOf := map[string]string{}
	if readable {
		var paths, ids, contained []string
		schema := false
		for _, it := range items {
			paths = append(paths, itemFile(it))
			if it.Record == store.RecordNode && it.After != nil && schemaKinds[it.Kind] {
				schema = true
				ids = append(ids, it.Identity)
				if it.Kind == "column" || it.Kind == "db_index" {
					contained = append(contained, it.Identity)
				}
			}
			// A code-to-table link's label needs its target's group tool
			// (row PL-F9 lists the tools it covers).
			if len(accessTargets(it)) > 0 {
				schema = true
			}
		}
		var err error
		if records, unrooted, err = rd.fileRecords(rd.repo, nonEmpty(paths)); err != nil {
			return nil, err
		}
		if schema {
			tables, err := rd.nodes("table")
			if err != nil {
				return nil, err
			}
			migrations, err := rd.nodes("migration")
			if err != nil {
				return nil, err
			}
			markerOf = schemaMarkers(tables, migrations)
			for _, t := range tables {
				nodes[t.Identity] = t
			}
			holders, err := rd.containers(contained)
			if err != nil {
				return nil, err
			}
			for id, e := range holders {
				tableOf[id] = e.Source
			}
			found, err := rd.byIdentity(ids)
			if err != nil {
				return nil, err
			}
			for id, n := range found {
				nodes[id] = n
			}
		}
	}
	routeFrameworks := map[string]string{}
	if readable {
		var targets []string
		for _, it := range items {
			if it.Record == store.RecordEdge && it.Kind == "exposes_route" {
				if i := strings.LastIndex(it.Name, " -> "); i >= 0 {
					targets = append(targets, it.Name[i+len(" -> "):])
				}
			}
		}
		if len(targets) > 0 {
			found, err := rd.byIdentity(targets)
			if err != nil {
				return nil, err
			}
			for id, n := range found {
				if f, ok := n.Attributes["framework"].(string); ok {
					routeFrameworks[id] = f
				}
			}
		}
	}
	for _, it := range items {
		run, side := head, "head"
		if it.Before != nil && base.Refusal() != "" {
			// Facts on the base side came from a run this table cannot
			// label: an external or another extractor version.
			run, side = base, "base"
		}
		entry := itemWithPrecision{Item: it, Precision: itemPrecision{Fields: map[string]*scopeLabel{}, ScopeSide: side}}
		if readable {
			entry.Precision.File = fileLabelOf(records[itemFile(it)], head, unrooted[itemFile(it)])
		}
		entry.Precision.Scope = itemScope(it, run, markerOf, nodes, tableOf, routeFrameworks)
		for _, f := range it.Fields {
			label := entry.Precision.Scope
			if f.Name == "table_access" {
				label = accessLabel(run, markerOf, nodes, fieldTargets(f))
			}
			if label != nil && label.Label == precision.Precise {
				for _, u := range label.Unverified {
					if f.Name == u {
						// A fact outside the row's scope (T023 GR10).
						label = notCompared()
					}
				}
			}
			entry.Precision.Fields[f.Name] = label
		}
		out = append(out, entry)
	}
	return out, nil
}

// itemScope decides the label of an item's record family.
func itemScope(it changes.Item, run precision.Run, markerOf func(contract.Node) precision.Marker, nodes map[string]contract.Node, tableOf map[string]string, routeFrameworks map[string]string) *scopeLabel {
	var family precision.Family
	switch it.Record {
	case store.RecordNode:
		family = nodeFamilies[it.Kind]
		if schemaKinds[it.Kind] {
			n, ok := nodes[it.Identity]
			if t, contained := tableOf[it.Identity]; ok && contained {
				n, ok = nodes[t]
			}
			if !ok || markerOf == nil || it.After == nil {
				reason := reasonScopeUnknown
				if refusal := run.Refusal(); refusal != "" {
					reason = refusal
				}
				return &scopeLabel{Label: precision.BestEffort, Row: precision.DefaultRow, Reason: &reason}
			}
			l := labelOf(run, markerOf(n))
			return &l
		}
	case store.RecordEdge:
		family = edgeFamilies[it.Kind]
		if family == precision.FamilyTableAccess {
			return accessLabel(run, markerOf, nodes, accessTargets(it))
		}
	}
	if family == "" {
		return nil
	}
	m := precision.Marker{Family: family}
	if family == familyRoutes {
		// Row PL-F10 labels Go routes by framework (T023 GO4): a route
		// item's own framework, an exposes_route edge's target route's.
		m.Tool = itemFramework(it, routeFrameworks)
	}
	l := labelOf(run, m)
	if it.Record == store.RecordEdge && l.Label == precision.Precise && len(rowUnverified[l.Row]) > 0 {
		// The handler edge of a precise route is outside the row's scope
		// (T023 GR10: no oracle compares handler identity).
		return notCompared()
	}
	return &l
}

// itemFramework is the framework of a route item (its head state, else
// its base state) or of an exposes_route item's target route; "" when
// unknown.
func itemFramework(it changes.Item, routeFrameworks map[string]string) string {
	if it.Record == store.RecordEdge {
		if i := strings.LastIndex(it.Name, " -> "); i >= 0 {
			return routeFrameworks[it.Name[i+len(" -> "):]]
		}
		return ""
	}
	for _, st := range []*changes.State{it.After, it.Before} {
		if st != nil {
			if f, ok := st.Fields["framework"].(string); ok {
				return f
			}
		}
	}
	return ""
}

// itemFile is the file an item's facts are in: the head side's, else the
// base side's.
func itemFile(it changes.Item) string {
	if it.After != nil && it.After.File != "" {
		return it.After.File
	}
	if it.Before != nil {
		return it.Before.File
	}
	return ""
}

func nonEmpty(list []string) []string {
	out := []string{}
	for _, s := range list {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// accessTargets is the target table of a reads_table or writes_table edge
// item ("source -> target" names it), none for any other item.
func accessTargets(it changes.Item) []string {
	if it.Record != store.RecordEdge || edgeFamilies[it.Kind] != precision.FamilyTableAccess {
		return nil
	}
	i := strings.LastIndex(it.Name, " -> ")
	if i < 0 {
		return nil
	}
	return []string{it.Name[i+len(" -> "):]}
}

// fieldTargets are the target tables of a table_access field's entries on
// both sides of the change.
func fieldTargets(f changes.Field) []string {
	var out []string
	for _, side := range []any{f.Before, f.After} {
		list, _ := side.([]any)
		for _, x := range list {
			if m, ok := x.(map[string]any); ok {
				if t, ok := m["target"].(string); ok {
					out = append(out, t)
				}
			}
		}
	}
	return out
}

// accessLabel labels a code-to-table link or table_access field by its
// targets' group tool; when a target's group cannot be read on the head
// side it is best_effort with scope_unknown (or the run's refusal).
func accessLabel(run precision.Run, markerOf func(contract.Node) precision.Marker, nodes map[string]contract.Node, targets []string) *scopeLabel {
	m, known := accessMarker(markerOf, nodes, targets)
	if !known {
		reason := reasonScopeUnknown
		if refusal := run.Refusal(); refusal != "" {
			reason = refusal
		}
		return &scopeLabel{Label: precision.BestEffort, Row: precision.DefaultRow, Reason: &reason}
	}
	l := labelOf(run, m)
	return &l
}

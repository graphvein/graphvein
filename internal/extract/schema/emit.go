package schema

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/testscope"
)

type emitter struct {
	nodes       map[string]contract.Node
	edges       map[string]*contract.Edge
	order       []string
	diagnostics []contract.Diagnostic
}

// qnPart quotes a name component that would make a qualified name
// ambiguous: one containing ".", a double quote, parentheses, ",", or "@"
// (the separators of table, column and index qualified names). Quoting uses
// SQL double-quote syntax.
func qnPart(name string) string {
	if strings.ContainsAny(name, `."(),@`) {
		return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	}
	return name
}

func tableQN(t *table) string {
	if t.q.schema != nil {
		return qnPart(*t.q.schema) + "." + qnPart(t.q.name)
	}
	return qnPart(t.q.name)
}

func newEmitter() *emitter {
	return &emitter{nodes: map[string]contract.Node{}, edges: map[string]*contract.Edge{}}
}

func (e *emitter) node(n contract.Node) {
	if _, dup := e.nodes[n.Identity]; dup {
		e.diagnostics = append(e.diagnostics, contract.Diagnostic{Type: "diagnostic", File: n.File, Severity: "warning", Code: "schema_duplicate_record", Message: fmt.Sprintf("%s: duplicate schema record %s skipped", n.File, n.Identity)})
		return
	}
	e.nodes[n.Identity] = n
}

// merge adds the records of one set's emitter, in its edge order.
func (e *emitter) merge(other *emitter) {
	ids := make([]string, 0, len(other.nodes))
	for id := range other.nodes {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		e.node(other.nodes[id])
	}
	for _, k := range other.order {
		ed := other.edges[k]
		e.edge(ed.Kind, ed.Source, ed.Target, ed.Evidence...)
	}
	e.diagnostics = append(e.diagnostics, other.diagnostics...)
}

func (e *emitter) edge(kind, source, target string, evidence ...contract.Evidence) {
	k := kind + "\x00" + source + "\x00" + target
	if existing, ok := e.edges[k]; ok {
		for _, ev := range evidence {
			if !slices.Contains(existing.Evidence, ev) {
				existing.Evidence = append(existing.Evidence, ev)
			}
		}
		return
	}
	if evidence == nil {
		evidence = []contract.Evidence{}
	}
	e.edges[k] = &contract.Edge{Type: "edge", Kind: kind, Source: source, Target: target, Provenance: "fact", Confidence: 1, Evidence: evidence}
	e.order = append(e.order, k)
}

func identity(kind, file, qualified string) string {
	id, _ := contract.Identity(kind, file, qualified)
	return id
}

func optional(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func strs(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

func names(cols []*column) []string {
	out := []string{}
	for _, c := range cols {
		out = append(out, c.name)
	}
	return out
}

func (set *migrationSet) migrationFor(file string) *migration {
	for _, m := range set.migrations {
		if m.file == file {
			return m
		}
	}
	return nil
}

func (e *emitter) emit(set *migrationSet) {
	var setOpaque any
	if set.dialect == "postgres" && !set.failed {
		total := 0
		byKind := map[string]any{}
		for k, v := range set.opaque {
			total += v
			byKind[k] = v
		}
		setOpaque = map[string]any{"total": total, "by_kind": byKind}
	}
	for _, m := range set.migrations {
		lines := strings.Split(string(m.source), "\n")
		whole := contract.Span{StartLine: 1, EndLine: len(lines), EndColumn: len(lines[len(lines)-1])}
		attrs := map[string]any{
			"tool": set.tool, "set": set.dir, "package": set.cargo, "dialect": set.dialect,
			"version": nil, "version_text": m.versionText, "direction": m.direction, "applied": nil, "order": nil,
			"description": m.description, "statements": nil, "modeled": nil, "no_effect": nil, "unsupported": nil,
			"opaque": nil, "errors": nil, "extensions": nil, "set_opaque_statements": setOpaque,
			"opaque_sites": nil,
		}
		if set.tool == toolSupabase {
			// SB-V2: the platform objects the set may use without
			// creating them, shown as the set's environment assumption.
			// SB-V3 and SB-V4 (T024c1b): a bare directory read as a
			// Supabase set, and each platform extension the set creates.
			attrs["platform"] = map[string]any{"name": "supabase", "assumption": supabaseAssumption, "objects": supabasePlatform, "assumptions": set.supabaseAssumptions()}
		}
		if m.version != nil {
			attrs["version"] = fmt.Sprintf("%d", *m.version)
		}
		if m.applied != nil {
			attrs["applied"] = *m.applied
		}
		// order is sqlx's apply position; it stays when applied is unknown
		// after a possible failure (WF8) and is null when undefined.
		if m.order > 0 {
			attrs["order"] = m.order
		}
		if m.analyzed {
			total := 0
			for _, v := range m.counts {
				total += v
			}
			attrs["statements"] = total
			attrs["modeled"] = m.counts[classModeled]
			attrs["no_effect"] = m.counts[classNoEffect]
			attrs["unsupported"] = m.counts[classUnsupported]
			attrs["opaque"] = m.counts[classOpaque]
			attrs["errors"] = m.counts[classError]
			attrs["extensions"] = strs(m.extensions)
			sites := []any{}
			for _, o := range m.opaqueSites {
				sites = append(sites, map[string]any{"span": m.span(o.at), "row": o.row, "kind": optional(nonEmpty(o.kind))})
			}
			attrs["opaque_sites"] = sites
		}
		if testscope.Classify(m.file, set.tests) == testscope.Test {
			attrs["scope"] = "test"
		}
		e.node(contract.Node{Type: "node", Identity: m.identity, Kind: "migration", Level: 3, Name: path.Base(m.file), QualifiedName: "migration", File: m.file, Span: &whole, ContentHash: contract.ContentHash(m.source), Attributes: attrs})
		e.edge("contains", identity("file", m.file, m.file), m.identity, contract.Evidence{File: m.file, Span: whole})
	}
	for _, t := range set.tables {
		e.table(set, t)
	}
}

func (e *emitter) table(set *migrationSet, t *table) {
	qn := tableQN(t)
	id := identity("table", t.file, qn)
	span := t.creator.span(t.span)
	state := "complete"
	reasons := []any{}
	for _, r := range t.reasons {
		state = "partial"
		entry := map[string]any{"migration": nil, "span": nil, "reason": set.toolText(r.text), "row": optional(nonEmpty(r.row))}
		if r.migration != nil {
			entry["migration"] = r.migration.identity
			entry["span"] = r.migration.span(r.span)
		}
		reasons = append(reasons, entry)
	}
	// primary_key (Revision 3 N2): the key's columns when known; [] when
	// the table is complete and has none; null (unknown) on a partial
	// table without a known key, where an unread statement may have added
	// one.
	var pk any = []string{}
	if t.primaryKey != nil {
		pk = names(t.primaryKey.cols)
	} else if state == "partial" {
		pk = nil
	}
	// unique_keys (Revision 4 WF3) is null once a DROP CONSTRAINT may have
	// dropped keys the pass cannot name; the keys added after it are not
	// listed then.
	var uniqueKeys any
	if !t.keysUnknown {
		list := []any{}
		for _, u := range t.uniques {
			list = append(list, map[string]any{"constraint": optional(u.name), "columns": names(u.cols)})
		}
		uniqueKeys = list
	}
	attrs := map[string]any{
		"name": t.q.name, "schema": optional(t.q.schema), "dialect": "postgres", "set": set.dir, "state": state,
		"reasons": reasons, "opaque_after": t.opaqueAfter, "primary_key": pk, "unique_keys": uniqueKeys,
		"previous_names": strs(t.previousNames),
	}
	if set.tool == toolSupabase {
		attrs["rls"], attrs["policies"] = rlsAttributes(set, t)
	}
	if testscope.Classify(t.file, set.tests) == testscope.Test {
		attrs["scope"] = "test"
	}
	source := set.sourceLookup[t.file]
	e.node(contract.Node{Type: "node", Identity: id, Kind: "table", Level: 4, Name: t.q.name, QualifiedName: qn, File: t.file, Span: &span, ContentHash: contract.ContentHash(source[t.span.start:t.span.end]), Attributes: attrs})
	e.edge("contains", t.creator.identity, id, contract.Evidence{File: t.file, Span: span})
	for _, a := range t.alterations {
		var evidence []contract.Evidence
		for _, s := range a.spans {
			evidence = append(evidence, contract.Evidence{File: a.migration.file, Span: a.migration.span(s)})
		}
		e.edge("alters", a.migration.identity, id, evidence...)
	}
	for i, c := range t.columns {
		e.column(set, t, qn, id, i+1, c)
	}
	unnamed := map[string]int{}
	for _, ix := range t.indexes {
		if ix.mayBeGone {
			// P3: a statement may have dropped it; its existence is unknown.
			continue
		}
		m := set.migrationFor(ix.file)
		ixSpan := m.span(ix.span)
		var label string
		elements := []string{}
		for _, el := range ix.elements {
			elements = append(elements, el.text)
		}
		if ix.name != nil {
			label = qnPart(*ix.name)
		} else {
			label = "(" + strings.Join(elements, ",") + ")"
			unnamed[label]++
			if n := unnamed[label]; n > 1 {
				label += fmt.Sprintf("@%d", n)
			}
		}
		name := label
		if ix.name != nil {
			name = *ix.name
		}
		ixID := identity("db_index", ix.file, qn+"."+label)
		attrs := map[string]any{"name": optional(ix.name), "unique": ix.unique, "using": optional(ix.using), "elements": elements, "include": strs(ix.include), "where": optional(ix.where)}
		attrs["claims"] = claimList(ix.claims, indexClaimFacts, attrs)
		if testscope.Classify(ix.file, set.tests) == testscope.Test {
			attrs["scope"] = "test"
		}
		e.node(contract.Node{Type: "node", Identity: ixID, Kind: "db_index", Level: 4, Name: name, QualifiedName: qn + "." + label, File: ix.file, Span: &ixSpan, ContentHash: contract.ContentHash(m.source[ix.span.start:ix.span.end]), Attributes: attrs})
		e.edge("contains", id, ixID, contract.Evidence{File: ix.file, Span: ixSpan})
	}
}

func columnIdentity(t *table, c *column) string {
	return identity("column", c.file, tableQN(t)+"."+qnPart(c.name))
}

func (e *emitter) column(set *migrationSet, t *table, qn, tableID string, position int, c *column) {
	m := set.migrationFor(c.file)
	span := m.span(c.span)
	id := columnIdentity(t, c)
	// unique (G3): true when the column alone is a primary key, a UNIQUE
	// constraint or the key of a unique index without predicate or
	// expression; false when the table is complete and none of these
	// exists; null (unknown) on a partial table.
	covered := uniqueAlone(t, c)
	var unique any = covered
	if !covered && len(t.reasons) > 0 {
		unique = nil
	}
	// Revision 3 N2: like unique, the facts that say "absent" are unknown
	// on a partial table (an unread or uncertain statement may have added
	// NOT NULL or a primary key); the facts source proves stay.
	inKey := inPrimaryKey(t, c)
	var nullable, primaryKey any = c.nullable, inKey
	if len(t.reasons) > 0 {
		if c.nullable {
			nullable = nil
		}
		if !inKey {
			primaryKey = nil
		}
	}

	var refs []any
	for _, fk := range t.foreignKeys {
		at := slices.Index(fk.local, c)
		if at < 0 {
			continue
		}
		ref := map[string]any{"constraint": optional(fk.name), "on_delete": optional(fk.onDelete), "on_update": optional(fk.onUpdate), "local_columns": names(fk.local)}
		fm := set.migrationFor(fk.file)
		evidence := contract.Evidence{File: fk.file, Span: fm.span(fk.span)}
		switch {
		case fk.table != nil:
			ref["table"], ref["columns"], ref["status"] = tableQN(fk.table), names(fk.columns), "linked"
			e.edge("foreign_key", id, columnIdentity(fk.table, fk.columns[at]), evidence)
		case fk.external != "":
			// SB-M2: a Supabase platform table, outside the set (SB-V2).
			ref["table"], ref["columns"], ref["status"] = fk.external, []string{"id"}, "external"
			ref["assumes"] = []string{supabaseAssumption}
		default:
			ref["table"], ref["columns"], ref["status"] = fk.written.table.String(), fk.written.columns, "unresolved"
			if fk.written.columns == nil {
				ref["columns"] = nil
			}
			ref["reason"] = fk.why
		}
		refs = append(refs, ref)
	}
	attrs := map[string]any{
		"name": c.name, "position": position, "type": c.typ, "nullable": nullable, "default": optional(c.def),
		"collation": optional(c.collation), "unique": unique,
		"primary_key": primaryKey,
		"identity":    optional(c.identity), "generated": optional(c.generated), "references": nil,
		"previous_names": strs(c.previousNames),
	}
	if c.typeResolved != "" {
		attrs["type_resolved"] = c.typeResolved
	}
	if c.implied {
		attrs["default_implied"] = "sequence"
	}
	if len(refs) > 0 {
		attrs["references"] = refs[0]
	}
	if len(refs) > 1 {
		attrs["additional_references"] = refs[1:]
	}
	if testscope.Classify(c.file, set.tests) == testscope.Test {
		attrs["scope"] = "test"
	}
	// name is claimed only after a rename; exists always.
	claimed := map[string]any{"name": nil}
	for k, v := range attrs {
		if k != "name" {
			claimed[k] = v
		}
	}
	if _, renamed := c.claims["name"]; renamed {
		claimed["name"] = c.name
	}
	attrs["claims"] = claimList(c.claims, columnClaimFacts, claimed)
	e.node(contract.Node{Type: "node", Identity: id, Kind: "column", Level: 4, Name: c.name, QualifiedName: qn + "." + qnPart(c.name), File: c.file, Span: &span, ContentHash: contract.ContentHash(m.source[c.span.start:c.span.end]), Attributes: attrs})
	e.edge("contains", tableID, id, contract.Evidence{File: c.file, Span: span})
}

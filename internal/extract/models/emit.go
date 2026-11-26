package models

import (
	"fmt"
	"sort"

	"github.com/graphvein/graphvein/internal/contract"
)

// emit writes the table and column nodes and their contains and
// foreign_key edges; nodes are sorted by identity.
func emit(tables []*Table, in LinkInput) ([]contract.Node, []contract.Edge) {
	var nodes []contract.Node
	var edges []contract.Edge
	ids := map[string]bool{}
	for _, t := range tables {
		var tn []contract.Node
		var te []contract.Edge
		in.Steps.Guard(t.File, "emit", "model table "+t.Identity, func() { tn, te = emitTable(t, ids, in) })
		nodes = append(nodes, tn...)
		edges = append(edges, te...)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Identity < nodes[j].Identity })
	return nodes, edges
}

func completeness(b bool) string {
	if b {
		return "complete"
	}
	return "partial"
}

func verification(own, group string) string {
	if own != "" {
		return own
	}
	return group
}

// knownVerification says whether v is a verification value the Schema
// view can show. Anything else (an empty value: a front end that set none
// and a group without a default) is a front-end bug, and it must never
// look verified (T018g0 Revision 2).
func knownVerification(v string) bool {
	return v == VerificationOracle || v == VerificationMapped
}

// emitTable writes one table's nodes and edges; ids holds the identities
// written so far.
func emitTable(t *Table, ids map[string]bool, in LinkInput) ([]contract.Node, []contract.Edge) {
	var nodes []contract.Node
	var edges []contract.Edge
	in.tag(in.Rows.Emit)
	if ids[t.Identity] {
		in.Steps.Diag(t.File, "warning", "schema_duplicate_record", fmt.Sprintf("%s: duplicate schema record %s skipped", t.File, t.Identity))
		return nil, nil
	}
	ids[t.Identity] = true
	span := t.Span
	g := in.Group
	reasons := t.Reasons
	if reasons == nil {
		reasons = []Reason{}
	}
	var schema any
	if t.Schema != nil {
		schema = *t.Schema
	}
	attrs := map[string]any{
		"source": "model", "tool": g.Tool, "set": g.SetKey(), "name": t.Name, "schema": schema, "schema_known": t.SchemaKnown,
		"state": completeness(t.ColumnsComplete && t.ConstraintsComplete), "reasons": reasons,
		"column_set": completeness(t.ColumnsComplete), "constraint_set": completeness(t.ConstraintsComplete),
		"primary_key": nil, "unique_constraints": nil, "indexes": nil, "check_constraints": nil,
		"assumptions": append([]string(nil), g.Assumptions...), "pins": g.Pins,
		"verification": tableVerification(t, in),
	}
	dialects := groupDialects(g)
	switch {
	case len(dialects) > 0:
		// A run-time dialect choice (ADR 0065): the table lists the
		// dialects, and no single dialect is written.
		attrs["dialects"] = dialects
		if g.Dialect != "" {
			internalError(in, t.File, "model table %s: group %s states dialect %s and run-time dialects %v; the single dialect is not written", t.Identity, g.SetKey(), g.Dialect, dialects)
		}
	case g.Dialect != "":
		attrs["dialect"] = g.Dialect
	}
	for k, v := range t.Attributes {
		attrs[k] = v
	}
	if t.ColumnsComplete && !t.PrimaryKeyOpen {
		pk := []string{}
		known := true
		for _, c := range t.Columns {
			switch c.Attributes["primary_key"] {
			case true:
				pk = append(pk, c.Name)
			case nil:
				known = false
			}
		}
		if known {
			attrs["primary_key"] = primaryKeyOrder(t, pk, in)
		}
	}
	// The elements read are listed; constraint_set says whether the lists
	// are complete, so an empty list never states an absence on a partial
	// set.
	if !t.ElementsUnknown {
		attrs["unique_constraints"], attrs["indexes"], attrs["check_constraints"] = nonNil(t.Uniques), nonNil(t.Indexes), nonNil(t.Checks)
	}
	if t.Test {
		attrs["scope"] = "test"
	}
	// verification_counts counts the claims of the columns written below by
	// verification, so /schema/sets can mark an unverified table without
	// reading every column's claims (ADR 0057). attrs is the map the table
	// node holds, so the loop's counts are in the record.
	counts := map[string]int{}
	attrs["verification_counts"] = counts
	nodes = append(nodes, contract.Node{Type: "node", Identity: t.Identity, Kind: "table", Level: 4, Name: t.Name, QualifiedName: t.Model, File: t.File, Span: &span, ContentHash: contract.ContentHash(Lines(in.Sources[t.File], span.StartLine, span.EndLine)), Attributes: attrs})
	if t.ModelIdentity != "" {
		edges = append(edges, contract.Edge{Type: "edge", Kind: "contains", Source: t.ModelIdentity, Target: t.Identity, Provenance: "fact", Confidence: 1, Evidence: []contract.Evidence{{File: t.File, Span: span}}})
	}
	for _, c := range t.Columns {
		id := ColumnIdentity(t, c)
		if ids[id] {
			in.Steps.Diag(t.File, "warning", "schema_duplicate_record", fmt.Sprintf("%s: duplicate schema record %s skipped", t.File, id))
			continue
		}
		ids[id] = true
		cspan := span
		if c.File == t.File {
			cspan = contract.Span{StartLine: c.Line, EndLine: c.Line}
		}
		a := map[string]any{"source": "model", "name": c.Name, "attribute": c.Attribute, "defined_in": map[string]any{"file": c.File, "line": c.Line}}
		for k, v := range c.Attributes {
			a[k] = v
		}
		// ADR 0065: the flat type family is checked, the per-dialect facts
		// written as by_dialect (their flat facts unknown) and the tool's
		// own type kept only as a string.
		checkTypeFamily(a, in, c.File, id)
		perDialect := dialectFacts(c, a, t, dialects, in)
		if v, has := a["tool_type"]; has {
			if _, isString := v.(string); !isString && v != nil {
				internalError(in, c.File, "%s: tool_type %v is not a string; dropped", id, v)
				delete(a, "tool_type")
			}
		}
		a["state"] = "read"
		if c.Unknown {
			a["state"] = "unknown"
		}
		cr := c.Reasons
		if cr == nil {
			cr = []Reason{}
		}
		a["reasons"] = cr
		switch {
		case c.Unknown || c.ForeignKeysUnknown:
			a["foreign_keys"] = nil
		default:
			list := []map[string]any{}
			for _, fk := range c.ForeignKeys {
				var schema any
				if fk.Schema != nil {
					schema = *fk.Schema
				}
				entry := map[string]any{"table": fk.Table, "schema": schema, "column": fk.Column, "on_delete": fk.OnDelete, "status": fk.Status}
				if fk.Columns != nil {
					var name any
					if fk.Name != nil {
						name = *fk.Name
					}
					entry["local_columns"] = append([]string{}, fk.Columns...)
					entry["columns"] = append([]string{}, fk.RefColumns...)
					entry["on_update"] = fk.OnUpdate
					entry["name"] = name
				}
				list = append(list, entry)
				if fk.Status == "resolved" {
					edges = append(edges, contract.Edge{Type: "edge", Kind: "foreign_key", Source: id, Target: fk.Target, Provenance: "fact", Confidence: 1, Evidence: []contract.Evidence{{File: fk.File, Span: contract.Span{StartLine: fk.Line, EndLine: fk.Line}}}})
				}
			}
			a["foreign_keys"] = list
		}
		columnClaims := claims(c, a, t, perDialect, dialects, in)
		for _, cl := range columnClaims {
			counts[cl.Verification]++
		}
		a["claims"] = columnClaims
		if t.Test {
			a["scope"] = "test"
		}
		csrc := Lines(in.Sources[c.File], c.Line, c.Line)
		nodes = append(nodes, contract.Node{Type: "node", Identity: id, Kind: "column", Level: 4, Name: c.Name, QualifiedName: t.Model + "." + identityName(c), File: t.File, Span: &cspan, ContentHash: contract.ContentHash(csrc), Attributes: a})
		edges = append(edges, contract.Edge{Type: "edge", Kind: "contains", Source: t.Identity, Target: id, Provenance: "fact", Confidence: 1, Evidence: []contract.Evidence{{File: t.File, Span: cspan}}})
	}
	return nodes, edges
}

// primaryKeyOrder returns the table's primary key: the source's order
// (Table.PrimaryKey) when it names exactly the key columns pk (in column
// order), else pk. A stated order that names other columns is a front-end
// bug: the key is written in column order with a
// schema_model_internal_error diagnostic, never in an order nobody stated.
func primaryKeyOrder(t *Table, pk []string, in LinkInput) []string {
	if t.PrimaryKey == nil {
		return pk
	}
	want := map[string]int{}
	for _, c := range pk {
		want[c]++
	}
	for _, c := range t.PrimaryKey {
		want[c]--
	}
	for _, n := range want {
		if n != 0 || len(t.PrimaryKey) != len(pk) {
			in.Steps.Diag(t.File, "warning", "schema_model_internal_error", fmt.Sprintf("%s: model table %s states primary key order %v for key columns %v; written in column order", t.File, t.Identity, t.PrimaryKey, pk))
			return pk
		}
	}
	return append([]string{}, t.PrimaryKey...)
}

// tableVerification is the verification of a table's own facts. A table
// whose front end and group state no known value is written mapped (never
// oracle) with a schema_model_internal_error diagnostic: the table's facts
// stay, unverified.
func tableVerification(t *Table, in LinkInput) string {
	v := verification(t.Verification, in.Group.Verification)
	if !knownVerification(v) {
		in.Steps.Diag(t.File, "warning", "schema_model_internal_error", fmt.Sprintf("%s: model table %s states no known verification (%q); written as %s", t.File, t.Identity, v, VerificationMapped))
		return VerificationMapped
	}
	return v
}

// claims lists the claims of a column's known facts (a: the record's
// attributes) in ClaimFacts order. A fact that is unknown in the record
// has no claim, whatever the front end recorded; an unknown column has
// none. A claim without a known verification is refused (T018g0 Revision
// 2): no claim, and a schema_model_internal_error diagnostic names it, so a
// front-end bug shows as a diagnostic and in the claim checks of its tests
// (modeltest.CheckClaims), never as a verified fact.
//
// A fact the column states per dialect (perDialect, ADR 0065) has one
// claim per group dialect whose value is known, at the fact's place in
// the order, in dialect order.
func claims(c *Column, a map[string]any, t *Table, perDialect map[string]bool, dialects []string, in LinkInput) []Claim {
	out := []Claim{}
	if c.Unknown {
		return out
	}
	for _, f := range ClaimFacts {
		if perDialect[f] {
			out = append(out, dialectClaims(c, a, f, t, dialects, in)...)
			continue
		}
		src, ok := c.Claims[f]
		if !ok || a[f] == nil {
			continue
		}
		v := verification(src.Verification, in.Group.Verification)
		if !knownVerification(v) {
			in.Steps.Diag(c.File, "warning", "schema_model_internal_error", fmt.Sprintf("%s: claim %s (row %s) of %s states no known verification (%q); refused", c.File, f, src.Row, ColumnIdentity(t, c), v))
			continue
		}
		out = append(out, newClaim(c, f, src, v, ""))
	}
	return out
}

// newClaim builds one claim; its file and line default to the column's.
func newClaim(c *Column, fact string, src ClaimSource, v, dialect string) Claim {
	file, line := src.File, src.Line
	if file == "" {
		file, line = c.File, c.Line
	}
	return Claim{Fact: fact, Dialect: dialect, Row: src.Row, File: file, Span: contract.Span{StartLine: line, EndLine: line}, Verification: v}
}

func nonNil(v []map[string]any) []map[string]any {
	if v == nil {
		return []map[string]any{}
	}
	return v
}

// Lines returns source lines first..last (1-based, inclusive).
func Lines(src []byte, first, last int) []byte {
	line, start := 1, -1
	for i := 0; i <= len(src); i++ {
		if line == first && start < 0 {
			start = i
		}
		if i == len(src) || src[i] == '\n' {
			if line == last && start >= 0 {
				return src[start:i]
			}
			line++
		}
	}
	if start >= 0 {
		return src[start:]
	}
	return nil
}

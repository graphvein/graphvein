package models

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// This file holds the two record capabilities of T018g0b (ADR 0065):
// per-dialect facts for a group whose dialect is chosen at run time, and a
// column type claimed as a type family whose modifiers are unknown. It also
// holds the tool-side type annotation (tool_type), which is not a database
// fact. A group that uses none of them emits exactly what it emitted
// before: every attribute below is written only when a front end sets it.

// DialectFacts are the column facts a front end may state once per dialect
// (Column.ByDialect) because the ORM renders them differently on each
// database: the SQL type and its modifiers, the type family, the
// autoincrement rendering and the server default. Every other fact is
// dialect-independent and stays single. server_default_text is text kept
// beside server_default and is covered by that fact's claim.
var DialectFacts = []string{
	"type", "type_family", "length", "timezone", "precision", "scale",
	"autoincrement", "server_default", "server_default_text",
}

// TypeModifiers are the modifiers a type family may leave unknown: length
// (character and bit types), precision (numeric precision, or the
// fractional-seconds precision of a time type), scale, and the generic
// ModifiersUnknown for a type whose modifiers are none of these.
var TypeModifiers = []string{"length", "precision", "scale", ModifiersUnknown}

// ModifiersUnknown is the generic unknown modifier of a type family: the
// type may take modifiers the source does not state and whose meaning is
// the type's own (an extension type such as PostGIS geometry(Point, 4326)
// or pgvector vector(1536), which Diesel's schema.rs writes without them;
// ADR 0062 Revision 2). It has no attribute of its own, so it is never
// cleared or checked as one, and a family that lists it lists nothing else.
const ModifiersUnknown = "modifiers"

// TypeFamily builds a type_family value: the type's family name as the
// ORM renders it without modifiers (for example "numeric" or "timestamp
// with time zone") and the modifiers that are unknown. The view shows it
// as "numeric (precision unknown)", never as the bare type: the database
// type has modifiers nobody can read from the source (Diesel's schema.rs
// drops them).
func TypeFamily(name string, unknown ...string) map[string]any {
	return map[string]any{"name": name, "unknown": append([]string{}, unknown...)}
}

// SetTypeFamily states the column's type as a family with unknown
// modifiers in one call: type_family is set and claimed by src, type and
// the unknown modifiers are cleared (unknown), so no reader sees the bare
// family as the type.
func (c *Column) SetTypeFamily(name string, unknown []string, src ClaimSource) {
	c.ensure()
	c.Attributes["type_family"] = TypeFamily(name, unknown...)
	c.Attributes["type"] = nil
	for _, m := range unknown {
		if m != ModifiersUnknown {
			c.Attributes[m] = nil
		}
	}
	c.Claims["type_family"] = src
}

// SetDialectFact states one dialect-dependent fact for one dialect in one
// call: the value and its claim (row, source and that dialect's
// verification). The flat fact of the same name is written unknown by the
// core, because no single value holds on every dialect.
func (c *Column) SetDialectFact(dialect, fact string, value any, src ClaimSource) {
	c.ensure()
	if c.ByDialect[dialect] == nil {
		c.ByDialect[dialect] = map[string]any{}
	}
	c.ByDialect[dialect][fact] = value
	if fact == "server_default_text" {
		// Text kept beside server_default: its claim covers it.
		return
	}
	if c.DialectClaims[dialect] == nil {
		c.DialectClaims[dialect] = map[string]ClaimSource{}
	}
	c.DialectClaims[dialect][fact] = src
}

// SetDialectTypeFamily states a type family with unknown modifiers for one
// dialect in one call (SetTypeFamily per dialect).
func (c *Column) SetDialectTypeFamily(dialect, name string, unknown []string, src ClaimSource) {
	c.SetDialectFact(dialect, "type_family", TypeFamily(name, unknown...), src)
	c.ByDialect[dialect]["type"] = nil
	for _, m := range unknown {
		c.ByDialect[dialect][m] = nil
	}
}

// SetToolType records the ORM's own type of the column as written in the
// source (Diesel's Nullable<Int4>). It is an annotation, not a database
// fact: it carries no claim and no verification, and the view labels it as
// the tool's type.
func (c *Column) SetToolType(text string) {
	c.ensure()
	c.Attributes["tool_type"] = text
}

func (c *Column) ensure() {
	if c.Attributes == nil {
		c.Attributes = map[string]any{}
	}
	if c.Claims == nil {
		c.Claims = map[string]ClaimSource{}
	}
	if c.ByDialect == nil {
		c.ByDialect = map[string]map[string]any{}
	}
	if c.DialectClaims == nil {
		c.DialectClaims = map[string]map[string]ClaimSource{}
	}
}

// groupDialects returns the group's run-time dialects, sorted, without
// empty or repeated names; a group that also states one dialect is a
// front-end bug: the single dialect is not written (diagnostic), because
// the run-time choice means no one dialect is proven.
func groupDialects(g Group) []string {
	var out []string
	for _, d := range g.Dialects {
		if d != "" && !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	sort.Strings(out)
	return out
}

// internalError writes a schema_model_internal_error diagnostic for a
// record a front end built against the kit's rules.
func internalError(in LinkInput, file, format string, args ...any) {
	in.Steps.Diag(file, "warning", "schema_model_internal_error", fmt.Sprintf("%s: ", file)+fmt.Sprintf(format, args...))
}

// checkTypeFamily enforces the type-family rules on one fact map (a
// column's attributes or one dialect's entry): a type_family is an object
// with a non-empty name and a non-empty list of distinct TypeModifiers
// (ModifiersUnknown only alone); a
// type beside it, or a value for a modifier it lists unknown, is a
// front-end bug. Every violation writes the facts unknown (null) with a
// diagnostic, so a family is never shown as more than it is.
func checkTypeFamily(m map[string]any, in LinkInput, file, subject string) {
	raw, has := m["type_family"]
	if !has || raw == nil {
		return
	}
	name, unknown, ok := typeFamilyParts(raw)
	if !ok {
		internalError(in, file, "%s: type_family %v is not a family name with unknown modifiers; written unknown", subject, raw)
		m["type_family"] = nil
		return
	}
	if m["type"] != nil {
		internalError(in, file, "%s: type %v beside type_family %s; both written unknown", subject, m["type"], name)
		m["type"], m["type_family"] = nil, nil
		return
	}
	for _, mod := range unknown {
		if m[mod] != nil {
			internalError(in, file, "%s: type_family %s lists %s unknown but it is %v; written unknown", subject, name, mod, m[mod])
			m[mod] = nil
		}
	}
	m["type_family"] = TypeFamily(name, unknown...)
}

// typeFamilyParts reads a type_family value as TypeFamily builds it.
func typeFamilyParts(raw any) (string, []string, bool) {
	obj, isObject := raw.(map[string]any)
	if !isObject || len(obj) != 2 {
		return "", nil, false
	}
	name, isString := obj["name"].(string)
	if !isString || strings.TrimSpace(name) == "" {
		return "", nil, false
	}
	var unknown []string
	switch list := obj["unknown"].(type) {
	case []string:
		unknown = list
	case []any:
		for _, v := range list {
			s, isString := v.(string)
			if !isString {
				return "", nil, false
			}
			unknown = append(unknown, s)
		}
	default:
		return "", nil, false
	}
	if len(unknown) == 0 || len(unknown) > 1 && slices.Contains(unknown, ModifiersUnknown) {
		return "", nil, false
	}
	seen := map[string]bool{}
	for _, mod := range unknown {
		if seen[mod] || !slices.Contains(TypeModifiers, mod) {
			return "", nil, false
		}
		seen[mod] = true
	}
	return name, unknown, true
}

// dialectFacts writes a column's per-dialect facts into a (the record's
// attributes) as by_dialect and returns the facts it covers; nil when the
// column states none. The entry of every group dialect lists the same
// facts (null where that dialect's value is unknown), and the flat fact of
// each covered name is written unknown: no single value holds on every
// dialect. A dialect outside the group's, a fact outside DialectFacts or
// per-dialect facts in a group without run-time dialects are front-end
// bugs: dropped with a diagnostic (and the flat facts they name still
// written unknown). In a group with run-time dialects every flat
// dialect-dependent fact is a front-end bug too (T018g0b Revision 1, A3):
// written unknown with a diagnostic. A modifier a per-dialect family lists
// unknown counts as covered (A4), so its flat value is written unknown.
func dialectFacts(c *Column, a map[string]any, t *Table, dialects []string, in LinkInput) map[string]bool {
	subject := ColumnIdentity(t, c)
	if len(dialects) > 0 {
		// T018g0b Revision 1 (A3): a group with run-time dialects states
		// every dialect-dependent fact per dialect. A flat value would
		// claim one value on every dialect, which the front end has not
		// proven: it is written unknown.
		for _, f := range DialectFacts {
			if a[f] != nil {
				internalError(in, c.File, "%s: %s stated once (%v) in group %s with run-time dialects %v; written unknown (state it per dialect)", subject, f, a[f], in.Group.SetKey(), dialects)
				a[f] = nil
			}
		}
	}
	if len(c.ByDialect) == 0 {
		return nil
	}
	covered := map[string]bool{}
	for _, d := range sortedDialectKeys(c.ByDialect) {
		known := slices.Contains(dialects, d)
		if !known {
			internalError(in, c.File, "%s: per-dialect facts for %q, not a dialect of group %s %v; dropped", subject, d, in.Group.SetKey(), dialects)
		}
		for _, f := range sortedFactKeys(c.ByDialect[d]) {
			if !slices.Contains(DialectFacts, f) {
				internalError(in, c.File, "%s: %s is not a per-dialect fact; dropped", subject, f)
				continue
			}
			covered[f] = true
		}
	}
	// A type and its family, and a server default and its text, are one
	// fact each: a per-dialect half never stands beside a flat half.
	for _, pair := range [][2]string{{"type", "type_family"}, {"server_default", "server_default_text"}} {
		if covered[pair[0]] || covered[pair[1]] {
			covered[pair[0]], covered[pair[1]] = true, true
		}
	}
	// A modifier a per-dialect family lists unknown is covered too
	// (Revision 1, A4): a flat value beside it would state what the family
	// says nobody knows.
	for _, d := range sortedDialectKeys(c.ByDialect) {
		if _, unknown, ok := typeFamilyParts(c.ByDialect[d]["type_family"]); ok {
			for _, m := range unknown {
				if m != ModifiersUnknown {
					covered[m] = true
				}
			}
		}
	}
	for f := range covered {
		if a[f] != nil {
			internalError(in, c.File, "%s: %s stated both once (%v) and per dialect; the single value is written unknown", subject, f, a[f])
		}
		a[f] = nil
	}
	if c.Unknown || len(covered) == 0 || len(dialects) == 0 {
		return covered
	}
	out := map[string]any{}
	for _, d := range dialects {
		entry := map[string]any{}
		for f := range covered {
			entry[f] = c.ByDialect[d][f]
		}
		checkTypeFamily(entry, in, c.File, subject+" ("+d+")")
		out[d] = entry
	}
	a["by_dialect"] = out
	return covered
}

// dialectClaims lists the per-dialect claims of fact f, one per group
// dialect whose value is known, in dialect order.
func dialectClaims(c *Column, a map[string]any, f string, t *Table, dialects []string, in LinkInput) []Claim {
	byDialect, _ := a["by_dialect"].(map[string]any)
	var out []Claim
	for _, d := range dialects {
		entry, _ := byDialect[d].(map[string]any)
		src, ok := c.DialectClaims[d][f]
		if !ok || entry[f] == nil {
			continue
		}
		v := verification(src.Verification, in.Group.Verification)
		if !knownVerification(v) {
			internalError(in, c.File, "claim %s (%s, row %s) of %s states no known verification (%q); refused", f, d, src.Row, ColumnIdentity(t, c), v)
			continue
		}
		out = append(out, newClaim(c, f, src, v, d))
	}
	return out
}

func sortedDialectKeys(m map[string]map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedFactKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

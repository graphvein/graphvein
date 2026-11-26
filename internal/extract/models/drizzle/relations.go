package drizzle

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// Row DR-R1: drizzle-orm 0.45's relations(table, ({ one, many }) => ({...}))
// in the group's schema modules. They drive the relational query API only
// (developer decision 2026-11-09): each entry is listed in the source
// table's attribute orm_relations, never as a foreign_key edge and never
// as a database fact (no claim, no verification).

// relations reads the relations() declarations among the exported values
// of the group's modules (the ones drizzle({ schema }) sees) and counts
// the relations() calls of the package outside them (a marker).
func (g *group) relations() {
	byDecl := map[*typescript.ModelDeclaration]*table{}
	for _, t := range g.tables {
		byDecl[t.decl] = t
	}
	for _, v := range g.order {
		base, links, ok := chain(v.decl.Value)
		if !ok {
			continue
		}
		r := g.in.calleeRef(v.file, base.Callee)
		if r.Module != "drizzle-orm" || r.Export != "relations" {
			continue
		}
		v := v
		g.steps.Guard(v.file, "relations", "Drizzle relations "+v.file+"#"+v.decl.Name, func() { g.readRelations(v, base, links, byDecl) })
	}
	g.outsideRelations()
}

// readRelations reads one relations() call.
func (g *group) readRelations(v visibleDecl, base typescript.ModelExpr, links []link, byDecl map[*typescript.ModelDeclaration]*table) {
	line := v.decl.Span.StartLine
	if len(links) > 0 || len(base.Args) != 2 || base.Args[0].Kind != typescript.ModelIdentifier {
		g.unread(v.file, line, "%s: relations() is not relations(<table>, <arrow>) (row D)", v.decl.Name)
		return
	}
	src := g.relationTable(v.file, base.Args[0].Name, byDecl)
	if src == nil {
		g.tag("DR-R1")
		g.steps.Diag(v.file, "info", codeUnread, fmt.Sprintf("%s:%d: %s: relations() of %s, which is not a table the group claims; not read (row DR-R1)", v.file, line, v.decl.Name, base.Args[0].Name))
		return
	}
	arrow := base.Args[1]
	if arrow.Kind != typescript.ModelArrow || len(arrow.Params) > 1 || arrow.Body == nil || arrow.Body.Kind != typescript.ModelObject {
		g.relationsUnread(src, v.file, line, "%s: the relations callback is not an arrow with at most one parameter returning an object literal", v.decl.Name)
		return
	}
	// `() => ({})` (dokploy) declares no relation; without a parameter
	// there are no helpers to call.
	list, _ := src.rec.Attributes["orm_relations"].([]map[string]any)
	if list == nil {
		list = []map[string]any{}
	}
	for _, en := range arrow.Body.Entries {
		if len(arrow.Params) == 0 {
			g.relationsUnread(src, v.file, en.Span.StartLine, "%s.%s: the relations callback has no parameter", v.decl.Name, en.Key)
			continue
		}
		param := arrow.Params[0]
		if en.Kind != typescript.ModelProperty {
			g.relationsUnread(src, v.file, en.Span.StartLine, "%s: a %s entry of the relations object is not read", v.decl.Name, en.Kind)
			continue
		}
		entry, why := g.relationEntry(v.file, src, param, en, byDecl)
		if why != "" {
			g.relationsUnread(src, v.file, en.Span.StartLine, "%s.%s: %s", v.decl.Name, en.Key, why)
			continue
		}
		g.tag("DR-R1")
		g.tag("DR-R1/" + entry["kind"].(string))
		list = append(list, entry)
	}
	src.rec.Attributes["orm_relations"] = list
}

// relationsUnread reports a relations() form no row reads (D) on the
// source table, as a reason (orm_relations lists only what is read).
func (g *group) relationsUnread(t *table, file string, line int, format string, a ...any) {
	g.tag("D")
	t.rec.Reasons = append(t.rec.Reasons, reasonAt("D", "orm_relations", file, line, fmt.Sprintf(format, a...)))
	if _, ok := t.rec.Attributes["orm_relations"]; !ok {
		t.rec.Attributes["orm_relations"] = []map[string]any{}
	}
}

// relationTable resolves a table identifier of a relations() call to a
// claimed table of the group (aliases such as kaneo's
// `export const user = userTable` included).
func (g *group) relationTable(file, name string, byDecl map[*typescript.ModelDeclaration]*table) *table {
	r := g.in.lookup(file, name)
	if r.Decl == nil {
		return nil
	}
	return byDecl[r.Decl]
}

// relationEntry reads `k: one(T, { fields: [S.a], references: [T.b] })`,
// `k: one(T)` or `k: many(T, { relationName })`.
func (g *group) relationEntry(file string, src *table, param string, en typescript.ModelEntry, byDecl map[*typescript.ModelDeclaration]*table) (map[string]any, string) {
	call := en.Value
	if call.Kind != typescript.ModelCall || call.Callee == nil {
		return nil, "not a one() or many() call"
	}
	kind := ""
	switch c := call.Callee; {
	case param == "" && c.Kind == typescript.ModelIdentifier && (c.Name == "one" || c.Name == "many"):
		// ({ one, many }) destructures the helpers; a module binding of
		// the same name would be another function.
		if r := g.in.lookup(file, c.Name); r != (ref{}) {
			return nil, c.Name + " is not the callback's helper"
		}
		kind = c.Name
	case param != "" && c.Kind == typescript.ModelMember && c.Object != nil && c.Object.Kind == typescript.ModelIdentifier && c.Object.Name == param && (c.Name == "one" || c.Name == "many"):
		kind = c.Name
	default:
		return nil, "not a one() or many() call"
	}
	if len(call.Args) < 1 || len(call.Args) > 2 || call.Args[0].Kind != typescript.ModelIdentifier {
		return nil, kind + "() is not " + kind + "(<table>, config?)"
	}
	entry := map[string]any{"name": en.Key, "kind": kind, "source": src.rec.Identity, "target": nil, "table": nil, "columns": nil, "references": nil}
	target := g.relationTable(file, call.Args[0].Name, byDecl)
	if target != nil {
		entry["target"], entry["table"] = target.rec.Identity, target.rec.Name
	}
	if len(call.Args) == 1 {
		return entry, ""
	}
	cfg, ok := configValues(&call.Args[1])
	if !ok {
		return nil, "the config is not an object literal"
	}
	for k, v := range cfg {
		switch {
		case k == "relationName":
		case kind == "one" && (k == "fields" || k == "references"):
			owner := src
			if k == "references" {
				owner = target
			}
			entry[map[string]string{"fields": "columns", "references": "references"}[k]] = g.relationColumns(file, owner, v, byDecl)
		default:
			return nil, "config key " + k + " is not read"
		}
	}
	return entry, ""
}

// relationColumns reads a literal list `[T.a, ...]` of columns of owner;
// nil when a column is not known.
func (g *group) relationColumns(file string, owner *table, v typescript.ModelExpr, byDecl map[*typescript.ModelDeclaration]*table) any {
	if owner == nil || v.Kind != typescript.ModelArray {
		return nil
	}
	var out []string
	for _, el := range v.Elements {
		if el.Kind != typescript.ModelMember || el.Object == nil || el.Object.Kind != typescript.ModelIdentifier || g.relationTable(file, el.Object.Name, byDecl) != owner {
			return nil
		}
		var found *column
		for _, c := range owner.columns {
			if c.key == el.Name && !c.rec.Unknown {
				found = c
			}
		}
		if found == nil {
			return nil
		}
		out = append(out, found.rec.Name)
	}
	return out
}

// outsideRelations counts the relations() calls in the package's files
// outside the group's modules (kaneo's relations.ts): one marker naming
// them, never read (row DR-R1): module-level `const x = relations(...)`
// in a file that imports `relations` from drizzle-orm.
func (g *group) outsideRelations() {
	in := map[string]bool{}
	for _, m := range g.c.Modules {
		in[m.File] = true
	}
	dir := g.c.Package.Dir
	counts := map[string]int{}
	for file, f := range g.in.TypeScript {
		if in[file] || !(dir == "." || strings.HasPrefix(file, dir+"/")) || strings.Contains("/"+file, "/node_modules/") {
			continue
		}
		local := ""
		for _, imp := range f.Imports {
			if imp.Module != "drizzle-orm" {
				continue
			}
			for l, exported := range imp.Bindings {
				if exported == "relations" {
					local = l
				}
			}
		}
		if local == "" {
			continue
		}
		// Module-level calls are not in the call facts; the module's
		// bindings hold `const x = relations(...)`.
		if f.Routes == nil || len(f.Routes.Scopes) == 0 {
			continue
		}
		for _, b := range f.Routes.Scopes[0].Bindings {
			if b.Value.Kind == "call_expression" && b.Value.Object != nil && b.Value.Object.Kind == "identifier" && b.Value.Object.Name == local {
				counts[file]++
			}
		}
	}
	if len(counts) == 0 {
		return
	}
	var files []string
	total := 0
	for f, n := range counts {
		files = append(files, fmt.Sprintf("%s (%d)", f, n))
		total += n
	}
	sort.Strings(files)
	g.tag("DR-R1")
	g.steps.Diag(g.c.Path, "info", codeUnread, fmt.Sprintf("%s: %d relations() calls outside the schema modules are not read (row DR-R1): %s", g.c.Path, total, strings.Join(files, ", ")))
}

// reasonAt builds a table reason at file:line.
func reasonAt(row, fact, file string, line int, text string) models.Reason {
	return models.Reason{Row: row, Fact: models.Fact(fact), File: file, Line: line, Reason: text}
}

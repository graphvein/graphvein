package supabasetypes

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
)

// migrationSet is one Supabase migration set as SB-G2 compares it.
type migrationSet struct {
	name   string
	tables map[string]*migrationTable // by "<schema>.<name>"
}

// migrationTable is one table record of a set with its columns.
type migrationTable struct {
	identity string
	complete bool
	columns  map[string]map[string]any // column attributes by name
}

// supabaseSets reads the Supabase migration sets (tool supabase) from the
// migration pass's records, by the npm package nearest to their package
// attribute (as the table-access pass finds a set's package).
func supabaseSets(in Input) map[string][]*migrationSet {
	bySet := map[string]*migrationSet{}
	pkgOf := map[string]string{}
	for _, n := range in.Migrations {
		set, _ := n.Attributes["set"].(string)
		if n.Kind == "migration" && n.Attributes["tool"] == "supabase" {
			if p, ok := n.Attributes["package"].(string); ok {
				pkgOf[set] = p
			}
			if bySet[set] == nil {
				bySet[set] = &migrationSet{name: set, tables: map[string]*migrationTable{}}
			}
		}
	}
	tables := map[string]*migrationTable{}
	for _, n := range in.Migrations {
		set, _ := n.Attributes["set"].(string)
		s := bySet[set]
		if n.Kind != "table" || s == nil {
			continue
		}
		schema, _ := n.Attributes["schema"].(string)
		if schema == "" {
			schema = "public"
		}
		name, _ := n.Attributes["name"].(string)
		t := &migrationTable{identity: n.Identity, complete: n.Attributes["state"] == "complete", columns: map[string]map[string]any{}}
		s.tables[schema+"."+name] = t
		tables[n.Identity] = t
	}
	cols := map[string]contract.Node{}
	for _, n := range in.Migrations {
		if n.Kind == "column" {
			cols[n.Identity] = n
		}
	}
	for _, e := range in.Edges {
		if e.Kind != "contains" {
			continue
		}
		if t, ok := tables[e.Source]; ok {
			if c, ok := cols[e.Target]; ok {
				t.columns[c.Name] = c.Attributes
			}
		}
	}
	out := map[string][]*migrationSet{}
	var names []string
	for name := range bySet {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		p, ok := pkgOf[name]
		if !ok {
			continue
		}
		dir, ok := npmPackage(p, in.Sources)
		if !ok {
			continue
		}
		out[dir] = append(out[dir], bySet[name])
	}
	return out
}

// compare is SB-G2's branch: the package's migration sets are its schema
// source, and the generated types are only compared with them. Every
// difference is a supabase_types_disagreement diagnostic; nothing is
// resolved or merged. Platform schemas (SB-V2) and schemas the types file
// does not list are not compared; columns are compared only on complete
// migration tables.
func compare(file string, tf *typesFile, sets []*migrationSet, steps *models.Steps) {
	var setNames []string
	mig := map[string]*migrationTable{}
	for _, s := range sets {
		setNames = append(setNames, s.name)
		for k, t := range s.tables {
			mig[k] = t
		}
	}
	listed := map[string]bool{}
	var platform []string
	both, agree, partial := 0, 0, 0
	var onlyTypes, onlyMig []string
	disagree := func(line int, format string, args ...any) {
		steps.Diag(file, "info", codeDisagreement, fmt.Sprintf("%s:%d: ", file, line)+fmt.Sprintf(format, args...)+" (row SB-G2; the migration set is the schema source)")
	}
	for _, s := range tf.schemas {
		if platformSchemas[s.name] {
			if len(s.tables) > 0 {
				platform = append(platform, s.name)
			}
			continue
		}
		listed[s.name] = true
		for i := range s.tables {
			r := &s.tables[i]
			key := s.name + "." + r.name
			mt, ok := mig[key]
			if !ok {
				onlyTypes = append(onlyTypes, key)
				disagree(r.line, "table %s is in the generated types, not in the migration set", key)
				continue
			}
			both++
			if !mt.complete {
				partial++
				continue
			}
			if compareColumns(key, r, mt, disagree) {
				agree++
			}
		}
	}
	for key := range mig {
		schema := key[:strings.IndexByte(key, '.')]
		if listed[schema] && !hasTable(tf, key) {
			onlyMig = append(onlyMig, key)
		}
	}
	sort.Strings(onlyMig)
	for _, key := range onlyMig {
		disagree(tf.line, "table %s of the migration set is not in the generated types", key)
	}
	msg := fmt.Sprintf("%s: generated types compared with Supabase migration set %s (row SB-G2: not read as a schema source): %d tables in both, %d agree on columns, nullability and relationships, %d disagree, %d not compared (partial in the migration set); only in the generated types: [%s]; only in the migration set: [%s]",
		file, strings.Join(setNames, ", "), both, agree, both-agree-partial, partial, strings.Join(onlyTypes, ", "), strings.Join(onlyMig, ", "))
	if len(platform) > 0 {
		msg += "; platform schemas not compared: [" + strings.Join(platform, ", ") + "]"
	}
	steps.Diag(file, "info", codeCompared, msg)
}

func hasTable(tf *typesFile, key string) bool {
	for _, s := range tf.schemas {
		for _, r := range s.tables {
			if s.name+"."+r.name == key {
				return true
			}
		}
	}
	return false
}

// compareColumns compares one table present in both sources whose
// migration table is complete: column names, nullability where both state
// it, and each relationship against the migration's foreign keys (local
// columns, referenced table name and columns). It reports whether all
// agree.
func compareColumns(key string, r *relation, mt *migrationTable, disagree func(int, string, ...any)) bool {
	ok := true
	seen := map[string]bool{}
	for i, ins := range r.insert {
		row := r.row[i]
		seen[ins.name] = true
		a, has := mt.columns[ins.name]
		if !has {
			disagree(row.line, "column %s.%s is in the generated types, not in the migration set", key, ins.name)
			ok = false
			continue
		}
		if n, isBool := a["nullable"].(bool); isBool && row.nullable != nil && n != *row.nullable {
			disagree(row.line, "column %s.%s: nullable %t in the generated types (%s), %t in the migration set", key, ins.name, *row.nullable, row.typ, n)
			ok = false
		}
	}
	var missing []string
	for name := range mt.columns {
		if !seen[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	for _, name := range missing {
		disagree(r.line, "column %s.%s of the migration set is not in the generated types", key, name)
		ok = false
	}
	for _, rel := range r.rels {
		if !hasForeignKey(mt, rel) {
			disagree(rel.line, "relationship %s of %s (%s) -> %s (%s) matches no foreign key of the migration set", rel.name, key, strings.Join(rel.columns, ", "), rel.refRel, strings.Join(rel.refColumns, ", "))
			ok = false
		}
	}
	return ok
}

// hasForeignKey finds a foreign key of the migration table with the
// relationship's local columns, referenced table name (its last
// component) and referenced columns.
func hasForeignKey(mt *migrationTable, rel relationship) bool {
	if len(rel.columns) == 0 {
		return false
	}
	a := mt.columns[rel.columns[0]]
	if a == nil {
		return false
	}
	var refs []any
	if r, ok := a["references"].(map[string]any); ok {
		refs = append(refs, r)
	}
	if list, ok := a["additional_references"].([]any); ok {
		refs = append(refs, list...)
	}
	for _, raw := range refs {
		r, _ := raw.(map[string]any)
		if r == nil {
			continue
		}
		table, _ := r["table"].(string)
		if i := strings.LastIndexByte(table, '.'); i >= 0 {
			table = table[i+1:]
		}
		if strings.Trim(table, `"`) == rel.refRel && sameStrings(r["local_columns"], rel.columns) && sameStrings(r["columns"], rel.refColumns) {
			return true
		}
	}
	return false
}

func sameStrings(v any, want []string) bool {
	var got []string
	switch l := v.(type) {
	case []string:
		got = l
	case []any:
		for _, x := range l {
			s, _ := x.(string)
			got = append(got, s)
		}
	default:
		return false
	}
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

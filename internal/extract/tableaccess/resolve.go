package tableaccess

import (
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
)

// This file holds the schema sources a SQL site resolves against (the
// sqlx migration sets of T018a, read from the schema records) and row
// CT-Q2, name resolution.

// table is one table node of a schema source.
type table struct {
	identity string
	// schema is the written qualification ("" when unqualified: the
	// database's search_path decides, public under TA-V4).
	schema, name string
	// pathUnproven is set for an unqualified table that carries a
	// search_path reason (O3.P13): it may live outside public (Revision 2).
	pathUnproven bool
}

// migrationSet is one sqlx migration set with the tables it holds at its
// end (the schema front end emits only those).
type migrationSet struct {
	name, pkg, dialect string
	// tool is the set's tool attribute (sqlx, supabase, ...).
	tool   string
	opaque int
	tables []table
	// files are the members sqlx applies, in its order (the oracle applies
	// them).
	files []string
	// prismaSchema and prismaVersion name a Prisma group's schema (its path
	// in the repository) and pinned CLI version: the oracle builds the
	// group's schema with prisma migrate diff.
	prismaSchema, prismaVersion string
	// drizzleRun names a Drizzle group's drizzle-kit run for the oracle
	// (REPO|PKG|CONFIG|SCHEMAS|DIALECT|CASING|KIT|ORM, the run format of
	// scripts/oracle/drizzle_catalog.sh): the oracle builds the group's
	// schema with drizzle-kit export.
	drizzleRun string
}

// migrationSets reads the sets, their packages and tables from the schema
// records.
func migrationSets(nodes []contract.Node) map[string]*migrationSet {
	sets := map[string]*migrationSet{}
	type member struct {
		file    string
		version string
	}
	members := map[string][]member{}
	get := func(name string) *migrationSet {
		s := sets[name]
		if s == nil {
			s = &migrationSet{name: name}
			sets[name] = s
		}
		return s
	}
	for _, n := range nodes {
		set, _ := n.Attributes["set"].(string)
		if set == "" {
			continue
		}
		switch n.Kind {
		case "migration":
			s := get(set)
			if p, ok := n.Attributes["package"].(string); ok {
				s.pkg = p
			}
			if d, ok := n.Attributes["dialect"].(string); ok {
				s.dialect = d
			}
			if t, ok := n.Attributes["tool"].(string); ok {
				s.tool = t
			}
			if o, ok := n.Attributes["set_opaque_statements"].(map[string]any); ok {
				s.opaque = intValue(o["total"])
			}
			if dir, _ := n.Attributes["direction"].(string); dir != "down" {
				v, _ := n.Attributes["version"].(string)
				members[set] = append(members[set], member{n.File, v})
			}
		case "table":
			s := get(set)
			t := table{identity: n.Identity}
			t.name, _ = n.Attributes["name"].(string)
			t.schema, _ = n.Attributes["schema"].(string)
			if t.schema == "" {
				if reasons, ok := n.Attributes["reasons"].([]any); ok {
					for _, r := range reasons {
						if m, ok := r.(map[string]any); ok && m["row"] == "O3.P13" {
							t.pathUnproven = true
						}
					}
				}
			}
			s.tables = append(s.tables, t)
		}
	}
	for name, ms := range members {
		sort.SliceStable(ms, func(i, j int) bool { return versionLess(ms[i].version, ms[j].version) })
		for _, m := range ms {
			sets[name].files = append(sets[name].files, m.file)
		}
	}
	for _, s := range sets {
		sort.Slice(s.tables, func(i, j int) bool { return s.tables[i].identity < s.tables[j].identity })
	}
	return sets
}

// versionLess orders sqlx versions numerically (they are i64 values).
func versionLess(a, b string) bool {
	x, e1 := strconv.ParseInt(a, 10, 64)
	y, e2 := strconv.ParseInt(b, 10, 64)
	if e1 != nil || e2 != nil {
		return a < b
	}
	return x < y
}

func intValue(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// cargoPackage returns the Cargo.toml nearest to file among sources, "" if
// none (W7: a site resolves only against its own package's sources).
func cargoPackage(file string, sources map[string][]byte) string {
	dir := path.Dir(file)
	for {
		manifest := path.Join(dir, "Cargo.toml")
		if dir == "." {
			manifest = "Cargo.toml"
		}
		if _, ok := sources[manifest]; ok {
			return manifest
		}
		if dir == "." || dir == "/" || dir == "" {
			return ""
		}
		dir = path.Dir(dir)
	}
}

// resolution is CT-Q2's result for one reading.
type resolution struct {
	reads, writes map[string]map[string]bool // table identity -> operations
	// unresolved are the reasons of references that claim nothing.
	unresolved []unresolvedRef
	forms      []string
	// tables maps a claimed identity to its schema-qualified name, for the
	// oracle.
	tables map[string]string
}

type unresolvedRef struct {
	reason     string
	candidates []string
}

// resolveNames is row CT-Q2's branch: each reference of a reading bound to
// exactly one table of the package's sets.
func resolveNames(r Reading, sets []*migrationSet) resolution {
	out := resolution{reads: map[string]map[string]bool{}, writes: map[string]map[string]bool{}, tables: map[string]string{}}
	forms := map[string]bool{}
	for _, ref := range r.Refs {
		if ref.CTE {
			forms["shadows"] = true
			continue
		}
		schema := ref.Schema
		if schema == "" {
			schema = "public"
		} else {
			forms["a qualified name"] = true
		}
		if strings.ToLower(ref.Name) == ref.Name {
			forms["folded to lower case"] = true
		} else {
			forms["unless quoted"] = true
		}
		display := schema + "." + ref.Name
		var hits []table
		unproven := false
		for _, s := range sets {
			for _, t := range s.tables {
				if t.name != ref.Name || !(t.schema == schema || t.schema == "" && schema == "public") {
					continue
				}
				if t.schema == "" && t.pathUnproven {
					unproven = true
					continue
				}
				hits = append(hits, t)
			}
		}
		switch {
		case unproven:
			out.unresolved = append(out.unresolved, unresolvedRef{reason: ReasonNameUnresolved + ": " + display + " (schema not proven: the table carries a search_path reason)"})
		case len(hits) == 0:
			opaque := 0
			for _, s := range sets {
				opaque += s.opaque
			}
			detail := display
			if opaque > 0 {
				detail += " (the set has opaque statements that may create tables)"
			}
			out.unresolved = append(out.unresolved, unresolvedRef{reason: ReasonNameUnresolved + ": " + detail})
		case len(hits) > 1:
			var ids []string
			for _, h := range hits {
				ids = append(ids, h.identity)
			}
			out.unresolved = append(out.unresolved, unresolvedRef{reason: ReasonAmbiguousGroup + ": " + display, candidates: ids})
		default:
			target := out.reads
			if ref.Write {
				target = out.writes
			}
			if target[hits[0].identity] == nil {
				target[hits[0].identity] = map[string]bool{}
			}
			target[hits[0].identity][ref.Operation] = true
			name := hits[0].schema
			if name == "" {
				name = "public"
			}
			out.tables[hits[0].identity] = name + "." + hits[0].name
		}
	}
	for f := range forms {
		out.forms = append(out.forms, f)
	}
	sort.Strings(out.forms)
	return out
}

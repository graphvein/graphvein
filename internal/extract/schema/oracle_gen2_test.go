package schema

// Generator g2 (T018a2 Revision 1): g1's grammar plus valid and
// refused variants of each B1a precondition. Every production's tag starts
// with the contract row whose precondition it exercises.
//
//   - P1 names: references to a name the set creates later, created and
//     then dropped or renamed away, or shaped like a generated name of a set
//     table, from every statement kind (exact rows and the token rule), and
//     the same statements on a name the set holds;
//   - P2 namespaces: index-backed constraint names and RENAME TO targets
//     that are taken (tables, indexes, generated names, the generated name
//     of a renamed table's key) or free;
//   - P3 conditional forms: CREATE TABLE / INDEX IF NOT EXISTS over taken,
//     generated (collision digits, expression indexes) and free names, and
//     DROP INDEX IF EXISTS under another qualification;
//   - P10: a member the pass cannot read (not UTF-8, or left out of the
//     sources);
//   - K2: ADD VALUE with BEFORE/AFTER, IF NOT EXISTS, RENAME VALUE, the use
//     of a new label in the same transaction, after RENAME TO and DROP TYPE.

import (
	"fmt"
	"slices"
)

var productions2 = []production{
	{8, genP1, false},
	{3, genP1Objects, false},
	{4, genP2, false},
	{4, genP3, false},
	{4, genK2, false},
}

// unheld returns a name the set creates but does not hold now: a dropped
// or renamed-away table, a name created later (pending; the prelude
// creates it when the set never does, which is then E1), or a generated
// shape of a set table. valid is false when there is none.
func (g *gen) unheld() (name, kind string, ok bool) {
	var options []string
	if len(g.dropped) > 0 {
		options = append(options, "dropped")
	}
	if len(g.renamedAway) > 0 {
		options = append(options, "renamed")
	}
	options = append(options, "forward")
	if len(g.tables) > 0 {
		options = append(options, "shape")
	}
	switch k := pick(g, options); k {
	case "dropped":
		return pick(g, g.dropped), k, true
	case "renamed":
		return pick(g, g.renamedAway), k, true
	case "shape":
		t := g.table()
		return pick(g, []string{t.name + "_pkey", t.name + "_" + g.col(t).name + "_key", t.name + "_" + g.col(t).name + "_idx"}), k, true
	default:
		n := g.name("t")
		g.pending = append(g.pending, n)
		return n, "forward", true
	}
}

// genP1 references a relation name from one statement kind; part of the
// time the name is held (valid), otherwise unheld (P1 refusal). A name
// referenced forward that the set never creates becomes a prelude table
// (E1), so forward names are used only where a table is needed.
func genP1(g *gen) (string, string, bool) {
	kinds := []struct{ sql, row string }{
		{"SELECT * FROM %s", "N3.select"},
		{"INSERT INTO %s DEFAULT VALUES", "N3.insert"},
		{"COMMENT ON TABLE %s IS 'c'", "N1.comment_table"},
		{"GRANT SELECT ON %s TO gv_oracle_role", "N1.grant"},
		{"LOCK TABLE %s IN SHARE MODE", "N1.lock"},
		{"CREATE VIEW " + g.name("v") + " AS SELECT * FROM %s", "R1.view"},
		{"CREATE TABLE " + g.name("t") + " AS SELECT * FROM %s", "R1.create_table_as"},
		{"CREATE INDEX ON %s (id)", "M4.create_unnamed"},
		{"ALTER TABLE %s ADD COLUMN " + g.name("c") + " int", "M5a.add"},
		{"CREATE TABLE " + g.name("t") + " (c int REFERENCES %s)", "M3.references"},
		{"DROP TABLE %s", "M6.drop"},
		{"COMMENT ON INDEX %s IS 'i'", "N1.comment_index"},
		{"DROP INDEX %s", "M7.drop"},
		{"ALTER INDEX %s RENAME TO " + g.name("ix_"), "M8.rename"},
		{"REINDEX INDEX %s", "N3.reindex_index"},
		{"CREATE TABLE " + g.name("t") + " (LIKE %s)", "M1b.like"},
	}
	k := pick(g, kinds)
	needsIndex := k.row == "N1.comment_index" || k.row == "M7.drop" || k.row == "N3.reindex_index"
	name, variant := "", "held"
	if t := g.table(); t != nil && g.chance(0.4) {
		name = t.name
		if needsIndex {
			name = pick(g, []string{t.name + "_pkey", t.name + "_" + g.col(t).name + "_idx"})
			variant = "held_or_shape"
		}
		if g.chance(0.3) {
			// Rename it away first.
			old := t.name
			t.name = g.name("t")
			g.renamedAway = append(g.renamedAway, old)
			return fmt.Sprintf("ALTER TABLE %s RENAME TO %s", old, t.name), "M5i.rename_p1", true
		}
	} else {
		var ok bool
		for tries := 0; tries < 5; tries++ {
			if name, variant, ok = g.unheld(); !ok {
				return "", "", false
			}
			if !needsIndex || variant != "forward" {
				break
			}
			g.pending = g.pending[:len(g.pending)-1]
			ok = false
		}
		if !ok {
			return "", "", false
		}
	}
	if k.row == "M6.drop" {
		if t := g.findTable(name); t != nil {
			g.tables = slices.DeleteFunc(g.tables, func(x *gTable) bool { return x == t })
			g.dropped = append(g.dropped, name)
		}
	}
	return fmt.Sprintf(k.sql, name), k.row + "_p1_" + variant, true
}

// genP1Objects references types, functions and schemas the set creates
// later or dropped: a column of an enum type, a DEFAULT calling a
// function, a trigger function, a schema-qualified name.
func genP1Objects(g *gen) (string, string, bool) {
	switch g.r.IntN(5) {
	case 0:
		// An enum type created later (pending) or dropped.
		typ := ""
		if len(g.droppedEnums) > 0 && g.chance(0.5) {
			typ = pick(g, g.droppedEnums)
		} else {
			typ = g.name("e")
			g.pendingEnums = append(g.pendingEnums, typ)
		}
		return fmt.Sprintf("CREATE TABLE %s (status %s)", g.name("t"), typ), "M1.enum_type_p1", true
	case 1:
		// A function created later (pending) or dropped, as a DEFAULT.
		fn := ""
		if len(g.droppedFuncs) > 0 && g.chance(0.5) {
			fn = pick(g, g.droppedFuncs)
		} else {
			fn = g.name("f")
			g.pendingFuncs = append(g.pendingFuncs, fn)
		}
		return fmt.Sprintf("CREATE TABLE %s (n int DEFAULT %s())", g.name("t"), fn), "M1.default_function_p1", true
	case 2:
		// Create a pending function now, or drop one the set holds.
		if len(g.pendingFuncs) > 0 && g.chance(0.6) {
			fn := g.pendingFuncs[0]
			g.pendingFuncs = g.pendingFuncs[1:]
			g.funcs = append(g.funcs, fn)
			return "CREATE FUNCTION " + fn + "() RETURNS int LANGUAGE sql AS $$ SELECT 1 $$", "R6.function_p1", true
		}
		if len(g.funcs) > 0 {
			fn := pick(g, g.funcs)
			g.funcs = slices.DeleteFunc(g.funcs, func(x string) bool { return x == fn })
			g.droppedFuncs = append(g.droppedFuncs, fn)
			return "DROP FUNCTION " + fn, "R5.drop_function_p1", true
		}
		return "", "", false
	case 3:
		// Drop an enum the set holds (later uses are backward references).
		if len(g.enums) == 0 {
			return "", "", false
		}
		e := pick(g, g.enums)
		g.enums = slices.DeleteFunc(g.enums, func(x *gEnum) bool { return x == e })
		g.droppedEnums = append(g.droppedEnums, e.name)
		return "DROP TYPE " + e.name + " CASCADE", "R5.drop_type_p1", true
	default:
		// A SQL function body that reads a table the set does not hold.
		name, variant, ok := g.unheld()
		if t := g.table(); t != nil && g.chance(0.5) {
			name, variant, ok = t.name, "held", true
		}
		if !ok {
			return "", "", false
		}
		fn := g.name("f")
		g.funcs = append(g.funcs, fn)
		return fmt.Sprintf("CREATE FUNCTION %s() RETURNS bigint LANGUAGE sql AS $$ SELECT count(*) FROM %s $$", fn, name), "R1b.sql_body_p1_" + variant, true
	}
}

// genP2 writes names into the shared pg_class namespace: index-backed
// constraint names and RENAME TO targets that are taken or free, and the
// generated name of a renamed table's key.
func genP2(g *gen) (string, string, bool) {
	t := g.table()
	if t == nil {
		return "", "", false
	}
	taken := []string{t.name, t.name + "_pkey", t.name + "_" + g.col(t).name + "_key", t.name + "_" + g.col(t).name + "_idx", t.name + "_id_seq"}
	for k := range g.indexes {
		taken = append(taken, k)
	}
	slices.Sort(taken)
	name, variant := g.name("k"), "free"
	if g.chance(0.6) {
		name, variant = pick(g, taken), "taken"
	}
	switch g.r.IntN(5) {
	case 0:
		return fmt.Sprintf("CREATE TABLE %s (id int, CONSTRAINT %s PRIMARY KEY (id))", g.name("t"), name), "M1.constraint_name_p2_" + variant, true
	case 1:
		c := g.col(t)
		return fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s UNIQUE (%s)", t.name, name, c.name), "M5f.constraint_name_p2_" + variant, true
	case 2:
		var from []string
		for k := range g.indexes {
			from = append(from, k)
		}
		slices.Sort(from)
		from = append(from, t.name+"_pkey")
		return fmt.Sprintf("ALTER INDEX %s RENAME TO %s", pick(g, from), name), "M8.rename_p2_" + variant, true
	case 3:
		// The key's generated name keeps the old table name after a rename.
		old := t.name
		t.name = g.name("t")
		g.renamedAway = append(g.renamedAway, old)
		g.indexes[old+"_pkey"] = t.name
		return fmt.Sprintf("ALTER TABLE %s RENAME TO %s", old, t.name), "M5i.rename_p2", true
	default:
		if len(g.renamedAway) == 0 {
			return "", "", false
		}
		old := pick(g, g.renamedAway)
		if g.chance(0.5) {
			return fmt.Sprintf("DROP INDEX %s_pkey", old), "M7.drop_p2_old_generated", true
		}
		return fmt.Sprintf("CREATE INDEX %s_pkey ON %s (%s)", old, t.name, g.col(t).name), "M4.create_p2_old_generated", true
	}
}

// genP3 writes IF NOT EXISTS and IF EXISTS forms whose effect depends on
// whether a name is taken: by a table, view or sequence, by a generated
// name with collision digits or of an expression index, or not at all.
func genP3(g *gen) (string, string, bool) {
	t := g.table()
	if t == nil {
		return "", "", false
	}
	c := g.col(t)
	switch g.r.IntN(6) {
	case 0:
		// Two unnamed indexes on one column: the second is <t>_<c>_idx1.
		return fmt.Sprintf("CREATE INDEX ON %s (%s)", t.name, c.name), "M4.create_unnamed_p3", true
	case 1:
		// An expression index: <t>_lower_idx (FigureIndexColname).
		return fmt.Sprintf("CREATE INDEX ON %s (lower(%s::text))", t.name, c.name), "M4.create_expression_p3", true
	case 2:
		name := pick(g, []string{t.name + "_" + c.name + "_idx", t.name + "_" + c.name + "_idx1", t.name + "_lower_idx", t.name + "_expr_idx", t.name + "_pkey", g.name("ix_")})
		g.indexes[name] = t.name
		return fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s (%s)", name, t.name, c.name), "M4.if_not_exists_p3", true
	case 3:
		var names []string
		names = append(names, g.views...)
		names = append(names, g.seqs...)
		names = append(names, t.name, t.name+"_pkey", g.name("t"))
		name := pick(g, names)
		return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id int, note text)", name), "M1.if_not_exists_p3", true
	case 4:
		var names []string
		for k := range g.indexes {
			names = append(names, k)
		}
		slices.Sort(names)
		names = append(names, t.name+"_"+c.name+"_idx")
		return fmt.Sprintf("DROP INDEX IF EXISTS public.%s", pick(g, names)), "M7.drop_if_exists_p3", true
	default:
		return fmt.Sprintf("DO $$ BEGIN EXECUTE 'CREATE TABLE IF NOT EXISTS %s (x int)'; END $$", g.name("t")), "O1.do_p3", true
	}
}

// genK2 writes the enum forms of B2 as amended: ADD VALUE AFTER/BEFORE an
// existing or missing label (with and without IF NOT EXISTS), RENAME
// VALUE to an existing label, and uses of a label added in this
// transaction (as a DEFAULT, a cast, an INSERT) to a type created in an
// earlier migration or in this one.
func genK2(g *gen) (string, string, bool) {
	if len(g.enums) == 0 {
		return "", "", false
	}
	e := pick(g, g.enums)
	if _, ok := g.enumMig[e.name]; !ok {
		g.enumMig[e.name] = -1
	}
	switch g.r.IntN(5) {
	case 0:
		label := g.name("l")
		ine := ""
		if g.chance(0.4) {
			ine = "IF NOT EXISTS "
			if g.chance(0.5) {
				label = pick(g, e.labels)
			}
		}
		where := pick(g, []string{"BEFORE", "AFTER"})
		nb := pick(g, append(slices.Clone(e.labels), "nosuch"))
		if !slices.Contains(e.labels, label) && nb != "nosuch" {
			e.labels = append(e.labels, label)
			g.enumAdded = append(g.enumAdded, label)
		}
		return fmt.Sprintf("ALTER TYPE %s ADD VALUE %s'%s' %s '%s'", e.name, ine, label, where, nb), "R3.add_value_neighbor_k2", true
	case 1:
		if len(e.labels) < 2 {
			return "", "", false
		}
		return fmt.Sprintf("ALTER TYPE %s RENAME VALUE '%s' TO '%s'", e.name, e.labels[0], e.labels[1]), "R3.rename_value_existing_k2", true
	case 2:
		if len(g.enumAdded) == 0 {
			return "", "", false
		}
		l := pick(g, g.enumAdded)
		return fmt.Sprintf("CREATE TABLE %s (status %s DEFAULT '%s')", g.name("t"), e.name, l), "M1.enum_default_k2", true
	case 3:
		if len(g.enumAdded) == 0 {
			return "", "", false
		}
		return fmt.Sprintf("SELECT '%s'::%s", pick(g, g.enumAdded), e.name), "R3.unsafe_use_k2", true
	default:
		// A new type and a label added to it in the same migration: safe.
		name := g.name("e")
		ne := &gEnum{name: name, labels: []string{"a"}}
		g.enums = append(g.enums, ne)
		return fmt.Sprintf("CREATE TYPE %s AS ENUM ('a')", name), "R2.create_enum_k2", true
	}
}

// unreadableMember adds, to 3% of g2 histories, a member the pass cannot
// read (P10): not UTF-8 (sqlx refuses the whole set) or valid SQL left out
// of the sources (sqlx applies it; the pass cannot know).
func (g *gen) unreadableMember(migrations int) {
	if !g.chance(0.03) {
		return
	}
	name := fmt.Sprintf("%d_member.sql", migrations+1)
	data := []byte("CREATE TABLE \xff (id integer);\n")
	if g.chance(0.5) {
		data = []byte(fmt.Sprintf("CREATE TABLE %s (id int);\n", g.name("u")))
	}
	g.h.unreadable = append(g.h.unreadable, rawFile{Name: name, Data: data})
}

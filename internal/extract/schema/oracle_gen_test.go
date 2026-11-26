package schema

// Oracle harness, generated histories (T018a2 A4): a seeded, reproducible
// generator over the claim contract's rows, with valid and refused
// variants, deliberately out-of-contract forms (D) and A0's latent claims,
// and a minimiser for disagreements.

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
)

// generatorVersion is part of every generated case id; bump it when the
// grammar changes so recorded seeds stay reproducible.
const generatorVersion = "g1"

type genStmt struct {
	sql string
	tag string // contract row and variant, e.g. "M3.forward"
}

type genMigration struct {
	name  string
	noTx  bool
	stmts []genStmt
}

type genHistory struct {
	id      string
	fresh   bool
	prelude []string
	migs    []genMigration
	// cargo, workspace and unreadable carry a file-based history through
	// the minimiser (fromHistory).
	cargo, workspace string
	unreadable       []rawFile
}

func (g *genHistory) history() *oracleHistory {
	h := &oracleHistory{ID: g.id, FreshDB: g.fresh, Prelude: g.prelude, Tags: map[string][]string{}, Cargo: g.cargo, Workspace: g.workspace, Unreadable: g.unreadable}
	for _, m := range g.migs {
		var b strings.Builder
		if m.noTx {
			b.WriteString("-- no-transaction\n")
		}
		var tags []string
		for _, s := range m.stmts {
			b.WriteString(s.sql)
			if s.tag != "fixture.unlexed" && s.tag != "fixture.empty" {
				b.WriteString(";")
			}
			b.WriteString("\n")
			// A generated item may write several statements; each gets the
			// item's tag, so that outcome.Statement indexes the right one
			// (B1c follow-up: tags were assigned one statement late after
			// such an item).
			for n := statementCount(s); n > 0; n-- {
				tags = append(tags, s.tag)
			}
		}
		h.Files = append(h.Files, oracleFile{Name: m.name, SQL: b.String()})
		h.Tags[m.name] = tags
	}
	return h
}

// statementCount is the number of statements Postgres runs for one item:
// one for a fixture item that does not lex or is empty.
func statementCount(s genStmt) int {
	if s.tag == "fixture.unlexed" || s.tag == "fixture.empty" {
		return 1
	}
	statements, failure := lexStatements([]byte(s.sql + ";"))
	if failure != nil || len(statements) == 0 {
		return 1
	}
	return len(statements)
}

func (g *genHistory) clone() *genHistory {
	c := *g
	c.prelude = slices.Clone(g.prelude)
	c.migs = make([]genMigration, len(g.migs))
	for i, m := range g.migs {
		c.migs[i] = genMigration{name: m.name, noTx: m.noTx, stmts: slices.Clone(m.stmts)}
	}
	return &c
}

// The generator's own model of what the history has created so far. It
// need not be exact (the oracle decides); it steers most statements to
// valid forms.
type gCol struct {
	name, typ string
	notNull   bool
}

type gTable struct {
	name    string
	cols    []*gCol
	pk      []string
	uniques [][]string
	cons    []string // constraint names the set wrote
	fks     int
	parent  string
}

type gEnum struct {
	name   string
	labels []string
}

type gen struct {
	r       *rand.Rand
	h       *genHistory
	n       int
	tables  []*gTable
	dropped []string
	pending []string // names referenced forward, to be created later
	// pendingEnums are enum types used before the set creates them.
	pendingEnums []string
	enums        []*gEnum
	views        []string
	matviews     []string
	seqs         []string
	domains      []string
	funcs        []string
	trigfns      []string
	indexes      map[string]string // index name -> table
	schemas      []string
	inMig        *genMigration
	clean        bool
	// enumAdded are labels added in the current migration (unsafe to use
	// before commit, check_safe_enum_use).
	enumAdded []string
	// prods is the generator's grammar (g1's, or g2's with its additions).
	prods []production
	// The g2 grammar's state (oracle_gen2_test.go).
	renamedAway  []string
	droppedEnums []string
	droppedFuncs []string
	pendingFuncs []string
	enumMig      map[string]int // enum name -> migration index created in
	mig          int
}

func (g *gen) name(prefix string) string {
	g.n++
	return fmt.Sprintf("%s%d", prefix, g.n)
}

func (g *gen) chance(p float64) bool { return g.r.Float64() < p }

func pick[T any](g *gen, list []T) T { return list[g.r.IntN(len(list))] }

func (g *gen) table() *gTable {
	if len(g.tables) == 0 {
		return nil
	}
	return pick(g, g.tables)
}

func (g *gen) findTable(name string) *gTable {
	for _, t := range g.tables {
		if t.name == name {
			return t
		}
	}
	return nil
}

var genTypes = []string{"int", "integer", "bigint", "smallint", "text", "varchar(40)", "character varying(20)", "boolean", "numeric(10,2)", "timestamptz", "timestamp", "date", "uuid", "jsonb", "real", "double precision", "int4", "int8", "char(3)", "text[]", "serial", "bigserial", "bytea", "float"}

func (g *gen) colType() string { return pick(g, genTypes) }

// generate builds the history for one seed with generator g1.
func generate(seed uint64) *genHistory {
	return generateWith(seed, generatorVersion)
}

// generateWith builds the history for one seed with generator g1, g2, g3,
// g4, g5, g6 or g7 (g7 = g6's plus the Revision 1.5 productions,
// oracle_gen7_test.go; g6 = g5's plus the Revision 1.4 productions,
// oracle_gen6_test.go; g2 = g1's grammar plus Revision 1's B1a productions,
// oracle_gen2_test.go; g3 = g2's plus the B1b productions,
// oracle_gen3_test.go; g4 = g3's plus the Revision 1.3 productions,
// oracle_gen4_test.go; g5 = g4's plus the B2 productions,
// oracle_gen5_test.go). Each uses its own stream, so the recorded seeds of
// the others stay reproducible.
func generateWith(seed uint64, version string) *genHistory {
	g := &gen{r: rand.New(rand.NewPCG(seed, 0x7018a2)), indexes: map[string]string{}, prods: productions, enumMig: map[string]int{}}
	switch version {
	case "g2":
		g.r = rand.New(rand.NewPCG(seed, 0x7018a22))
		g.prods = append(slices.Clone(productions), productions2...)
	case "g3":
		g.r = rand.New(rand.NewPCG(seed, 0x7018a23))
		g.prods = append(append(slices.Clone(productions), productions2...), productions3...)
	case "g4":
		g.r = rand.New(rand.NewPCG(seed, 0x7018a24))
		g.prods = append(append(append(slices.Clone(productions), productions2...), productions3...), productions4...)
	case "g5":
		g.r = rand.New(rand.NewPCG(seed, 0x7018a25))
		g.prods = append(append(append(append(slices.Clone(productions), productions2...), productions3...), productions4...), productions5...)
	case "g6":
		g.r = rand.New(rand.NewPCG(seed, 0x7018a26))
		g.prods = append(append(append(append(append(slices.Clone(productions), productions2...), productions3...), productions4...), productions5...), productions6...)
	case "g7":
		// Revision 1.5: g6's productions plus productions7
		// (oracle_gen7_test.go).
		g.r = rand.New(rand.NewPCG(seed, 0x7018a27))
		g.prods = append(append(append(append(append(append(slices.Clone(productions), productions2...), productions3...), productions4...), productions5...), productions6...), productions7...)
	case "g8":
		// Revision 1.6: g7's productions plus productions8
		// (oracle_gen8_test.go).
		g.r = rand.New(rand.NewPCG(seed, 0x7018a28))
		g.prods = append(append(append(append(append(append(append(slices.Clone(productions), productions2...), productions3...), productions4...), productions5...), productions6...), productions7...), productions8...)
	case "g9":
		// Revision 1.9: g8's productions plus productions9
		// (oracle_gen9_test.go).
		g.r = rand.New(rand.NewPCG(seed, 0x7018a29))
		g.prods = append(append(append(append(append(append(append(append(slices.Clone(productions), productions2...), productions3...), productions4...), productions5...), productions6...), productions7...), productions8...), productions9...)
	}
	// Half of the g4 histories use only g4's productions, so that more of
	// their data changes reach a claim (an earlier possible failure makes
	// every later migration applied: null, and the oracle then judges none
	// of their values).
	focused := (version == "g4" || version == "g5" || version == "g6" || version == "g7" || version == "g8" || version == "g9") && g.chance(0.5)
	switch {
	case focused && version == "g4":
		g.prods = productions4
	case focused && version == "g5":
		g.prods = productions5
	case focused && version == "g6":
		g.prods = productions6
	case focused && version == "g8":
		g.prods = productions8
	case focused && version == "g9":
		g.prods = productions9
	case focused:
		g.prods = productions7
	}
	g.h = &genHistory{id: fmt.Sprintf("%s-s%d", version, seed)}
	g.h.prelude = []string{
		"CREATE TABLE ext_a (id int PRIMARY KEY, code text UNIQUE)",
		"CREATE TABLE ext_b (id bigint PRIMARY KEY)",
	}
	// A small share of histories may use schemas, search_path and
	// extensions; they run in a fresh database.
	g.h.fresh = g.chance(0.06)
	if focused && (version == "g7" || version == "g8" || version == "g9") && !g.h.fresh {
		// Revision 1.5 U3: search_path productions need schemas of the
		// set, which only a fresh database gives them.
		g.h.fresh = g.chance(0.25)
	}
	// Half of the histories avoid the deliberately refused and default
	// branch productions, so that more claims reach applied: true.
	g.clean = g.chance(0.5)
	migrations := 1 + g.r.IntN(8)
	if focused {
		migrations = 1 + g.r.IntN(3)
	}
	for i := 0; i < migrations; i++ {
		m := genMigration{name: fmt.Sprintf("%d_m%d.sql", i+1, i+1)}
		g.inMig, g.enumAdded, g.mig = &m, nil, i
		count := 1 + g.r.IntN(12)
		if focused {
			count = 1 + g.r.IntN(2)
		}
		if g.chance(0.08) {
			m.noTx = true
			if g.chance(0.6) {
				count = 1
			}
		}
		for j := 0; j < count; j++ {
			m.stmts = append(m.stmts, g.statement(m.noTx && j == 0))
		}
		g.h.migs = append(g.h.migs, m)
	}
	// Names referenced forward that were never created are outside the
	// set (E1): the prelude creates them with a key.
	for _, p := range g.pending {
		g.h.prelude = append(g.h.prelude, fmt.Sprintf("CREATE TABLE %s (id int PRIMARY KEY)", p))
	}
	for _, p := range g.pendingEnums {
		g.h.prelude = append(g.h.prelude, fmt.Sprintf("CREATE TYPE %s AS ENUM ('a')", p))
	}
	for _, p := range g.pendingFuncs {
		g.h.prelude = append(g.h.prelude, fmt.Sprintf("CREATE FUNCTION %s() RETURNS int LANGUAGE sql AS $$ SELECT 1 $$", p))
	}
	if version != "g1" {
		g.unreadableMember(migrations)
	}
	return g.h
}

type production struct {
	weight int
	fn     func(g *gen) (string, string, bool)
	// bad marks productions skipped in clean histories.
	bad bool
}

var productions []production

func init() {
	productions = []production{
		{30, genCreateTable, false},
		{4, genCreateTableBad, true},
		{3, genCreateTableDerived, false},
		{10, genCreateIndex, false},
		{20, genAlterTable, false},
		{4, genDropTable, false},
		{3, genDropIndex, false},
		{2, genAlterIndex, false},
		{4, genRelation, false},
		{5, genEnum, false},
		{3, genOtherAlter, false},
		{3, genOtherDrop, false},
		{3, genFunction, false},
		{6, genNoEffect, false},
		{4, genSession, false},
		{5, genData, false},
		{3, genOpaque, false},
		{2, genFailing, true},
		{3, genDefault, true},
		{2, genSchema, false},
	}
}

func (g *gen) statement(firstNoTx bool) genStmt {
	if firstNoTx && len(g.tables) > 0 && g.chance(0.7) {
		t := g.table()
		c := pick(g, t.cols)
		name := g.name("ix_")
		g.indexes[name] = t.name
		return genStmt{fmt.Sprintf("CREATE INDEX CONCURRENTLY %s ON %s (%s)", name, t.name, c.name), "M4.concurrently_notx"}
	}
	total := 0
	for _, p := range g.prods {
		total += p.weight
	}
	for tries := 0; tries < 50; tries++ {
		n := g.r.IntN(total)
		for _, p := range g.prods {
			if n < p.weight {
				if p.bad && g.clean {
					break
				}
				if sql, tag, ok := p.fn(g); ok {
					return genStmt{sql, tag}
				}
				break
			}
			n -= p.weight
		}
	}
	return genStmt{"SELECT 1", "N3.select"}
}

// columnDef writes one column of a new table.
func (g *gen) columnDef(t *gTable, c *gCol, refs bool) string {
	ref := ""
	if refs && g.chance(0.2) {
		ref = " " + g.reference(t, c, true)
	}
	s := c.name + " " + c.typ
	if g.chance(0.3) {
		s += " NOT NULL"
		c.notNull = true
	}
	if g.chance(0.15) && !strings.Contains(c.typ, "serial") {
		switch {
		case strings.Contains(c.typ, "int") || c.typ == "numeric(10,2)" || c.typ == "real" || c.typ == "float" || c.typ == "double precision":
			s += " DEFAULT 0"
		case c.typ == "boolean":
			s += " DEFAULT false"
		case strings.Contains(c.typ, "time") || c.typ == "date":
			s += " DEFAULT now()"
		case c.typ == "text" || strings.Contains(c.typ, "char"):
			s += " DEFAULT ''"
		}
	}
	return s + ref
}

// reference writes REFERENCES to an existing table's key (valid), to a
// non-key column (refused), to a table created later (K1), to a table
// outside the set (E1), or to a dropped name.
func (g *gen) reference(t *gTable, c *gCol, setType bool) string {
	typ := c.typ
	defer func() {
		if !setType {
			c.typ = typ
		}
	}()
	switch n := g.r.IntN(10); {
	case n < 5 && len(g.tables) > 0:
		target := g.table()
		if len(target.pk) == 1 {
			for _, tc := range target.cols {
				if tc.name == target.pk[0] {
					// The referencing column's type may differ (C2 pairs).
					c.typ = fkType(g, tc.typ)
				}
			}
			return "REFERENCES " + target.name
		}
		return "REFERENCES " + target.name + " (" + pick(g, target.cols).name + ")"
	case n < 7:
		name := g.name("t")
		g.pending = append(g.pending, name)
		c.typ = pick(g, []string{"int", "bigint", "integer"})
		return "REFERENCES " + name
	case n < 9:
		// Outside tables (E1) have the key types the set needs.
		switch g.r.IntN(3) {
		case 0:
			c.typ = pick(g, []string{"int", "bigint", "smallint", "integer"})
			return "REFERENCES ext_a"
		case 1:
			c.typ = pick(g, []string{"int", "bigint"})
			return "REFERENCES ext_b"
		default:
			c.typ = pick(g, []string{"text", "varchar(40)"})
			return "REFERENCES ext_a (code)"
		}
	default:
		if len(g.dropped) > 0 {
			return "REFERENCES " + pick(g, g.dropped)
		}
		return "REFERENCES ext_a"
	}
}

// fkType picks the referencing column's type for a key of type pk: mostly
// the same, sometimes another integer width, text or numeric (C2).
func fkType(g *gen, pk string) string {
	if g.chance(0.6) {
		switch pk {
		case "serial":
			return "integer"
		case "bigserial":
			return "bigint"
		}
		return pk
	}
	return pick(g, []string{"int", "bigint", "smallint", "int8", "numeric", "text", "varchar(40)", "integer"})
}

func (g *gen) newTableName() string {
	if len(g.pending) > 0 && g.chance(0.6) {
		name := g.pending[0]
		g.pending = g.pending[1:]
		return name
	}
	if len(g.dropped) > 0 && g.chance(0.1) {
		return pick(g, g.dropped)
	}
	return g.name("t")
}

func genCreateTable(g *gen) (string, string, bool) {
	name := g.newTableName()
	t := &gTable{name: name}
	tag := "M1.create"
	var elems []string
	idType := pick(g, []string{"int", "bigint", "serial", "bigserial", "integer", "uuid", "text"})
	id := &gCol{name: "id", typ: idType, notNull: true}
	t.cols = append(t.cols, id)
	pkInline := g.chance(0.7)
	if pkInline {
		elems = append(elems, "id "+idType+" PRIMARY KEY")
		t.pk = []string{"id"}
	} else {
		elems = append(elems, "id "+idType)
	}
	ncols := g.r.IntN(5)
	for i := 0; i < ncols; i++ {
		c := &gCol{name: fmt.Sprintf("c%d", i+1), typ: g.colType()}
		t.cols = append(t.cols, c)
		elems = append(elems, g.columnDef(t, c, true))
	}
	if len(g.enums) > 0 && g.chance(0.3) {
		e := pick(g, g.enums)
		c := &gCol{name: "status", typ: e.name}
		t.cols = append(t.cols, c)
		d := ""
		if g.chance(0.5) && len(e.labels) > 0 {
			d = " DEFAULT '" + pick(g, append(e.labels, g.enumAdded...)) + "'"
			tag = "M1.enum_default"
		}
		elems = append(elems, "status "+e.name+d)
	}
	if !pkInline && g.chance(0.6) {
		cons := ""
		if g.chance(0.4) {
			cn := name + pick(g, []string{"_pk", "_pkey", "_primary"})
			cons = "CONSTRAINT " + cn + " "
			t.cons = append(t.cons, cn)
		}
		elems = append(elems, cons+"PRIMARY KEY (id)")
		t.pk = []string{"id"}
	}
	if len(t.cols) > 1 && g.chance(0.3) {
		c := t.cols[1+g.r.IntN(len(t.cols)-1)]
		cons := ""
		if g.chance(0.5) {
			cn := name + "_" + c.name + pick(g, []string{"_key", "_uq", "_unique"})
			cons = "CONSTRAINT " + cn + " "
			t.cons = append(t.cons, cn)
		}
		elems = append(elems, cons+"UNIQUE ("+c.name+")")
		t.uniques = append(t.uniques, []string{c.name})
	}
	if len(t.cols) > 1 && g.chance(0.2) {
		c := t.cols[1+g.r.IntN(len(t.cols)-1)]
		cons := ""
		if g.chance(0.5) {
			cn := name + "_" + c.name + "_fk"
			cons = "CONSTRAINT " + cn + " "
			t.cons = append(t.cons, cn)
		}
		elems = append(elems, cons+"FOREIGN KEY ("+c.name+") "+g.reference(t, c, false))
		tag = "M1.create_fk"
	}
	if g.chance(0.15) {
		elems = append(elems, "CHECK (id IS NOT NULL)")
	}
	unlogged := ""
	if g.chance(0.05) {
		unlogged = "UNLOGGED "
	}
	g.tables = append(g.tables, t)
	g.dropped = slices.DeleteFunc(g.dropped, func(s string) bool { return s == name })
	return fmt.Sprintf("CREATE %sTABLE %s (%s)", unlogged, name, strings.Join(elems, ", ")), tag, true
}

func genCreateTableBad(g *gen) (string, string, bool) {
	switch g.r.IntN(7) {
	case 0:
		if t := g.table(); t != nil {
			return fmt.Sprintf("CREATE TABLE %s (id int)", t.name), "M1.taken", true
		}
	case 1:
		if t := g.table(); t != nil {
			return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id int, extra text)", t.name), "M1.if_not_exists_existing", true
		}
	case 2:
		name := g.name("t")
		return fmt.Sprintf("CREATE TABLE %s (a int, a text)", name), "M1.duplicate_column", true
	case 3:
		name := g.name("t")
		return fmt.Sprintf("CREATE TABLE %s (a int, PRIMARY KEY (nosuch))", name), "M1.key_column_missing", true
	case 4:
		if len(g.views)+len(g.enums)+len(g.seqs) > 0 {
			names := append(append(slices.Clone(g.views), g.seqs...), enumNames(g)...)
			return fmt.Sprintf("CREATE TABLE %s (id int)", pick(g, names)), "M1.name_taken_relation", true
		}
	case 5:
		if t := g.table(); t != nil {
			name := g.name("t")
			g.tables = append(g.tables, &gTable{name: name, cols: []*gCol{{name: "id", typ: "int"}}, pk: []string{"id"}})
			return fmt.Sprintf("CREATE TABLE %s (id int, CONSTRAINT %s_pkey PRIMARY KEY (id))", name, t.name), "M1.generated_name_shape", true
		}
	case 6:
		name := g.name("t")
		return fmt.Sprintf("CREATE TABLE %s (id int primary key, x int int)", name), "D.unreadable_type", true
	}
	return "", "", false
}

func enumNames(g *gen) []string {
	var out []string
	for _, e := range g.enums {
		out = append(out, e.name)
	}
	return out
}

func genCreateTableDerived(g *gen) (string, string, bool) {
	t := g.table()
	if t == nil {
		return "", "", false
	}
	name := g.name("t")
	switch g.r.IntN(4) {
	case 0:
		g.tables = append(g.tables, &gTable{name: name, cols: slices.Clone(t.cols)})
		return fmt.Sprintf("CREATE TABLE %s (LIKE %s INCLUDING ALL)", name, t.name), "M1b.like", true
	case 1:
		g.tables = append(g.tables, &gTable{name: name, cols: slices.Clone(t.cols), parent: t.name})
		return fmt.Sprintf("CREATE TABLE %s (extra int) INHERITS (%s)", name, t.name), "M1b.inherits", true
	case 2:
		g.relationsAdd(name)
		return fmt.Sprintf("CREATE TABLE %s AS SELECT * FROM %s", name, t.name), "R1.create_table_as", true
	default:
		return fmt.Sprintf("SELECT * INTO %s FROM %s", name, t.name), "R1.select_into", true
	}
}

func (g *gen) relationsAdd(name string) { g.views = append(g.views, name) }

func genCreateIndex(g *gen) (string, string, bool) {
	t := g.table()
	if t == nil {
		return "", "", false
	}
	c := pick(g, t.cols)
	unique := ""
	if g.chance(0.2) {
		unique = "UNIQUE "
	}
	conc := ""
	tag := "M4.create"
	if g.chance(0.05) {
		conc, tag = "CONCURRENTLY ", "M4.concurrently_tx"
	}
	ine := ""
	if g.chance(0.15) {
		ine = "IF NOT EXISTS "
	}
	target := t.name
	col := c.name
	switch n := g.r.IntN(20); {
	case n == 0:
		col, tag = "nosuch", "M4.column_missing"
	case n == 1:
		target = g.name("t")
		g.pending = append(g.pending, target)
		col, tag = "id", "M4.forward_table"
	case n == 2:
		target, col, tag = "ext_a", "code", "M4.outside_table"
	}
	where := ""
	if g.chance(0.1) {
		where = " WHERE " + col + " IS NOT NULL"
	}
	switch n := g.r.IntN(10); {
	case n < 4:
		return fmt.Sprintf("CREATE %sINDEX %s%sON %s (%s)%s", unique, conc, ine, target, col, where), tag + "_unnamed", true
	case n < 5:
		// A user name shaped like a generated name (C5).
		name := fmt.Sprintf("%s_%s_idx", target, col)
		g.indexes[name] = target
		return fmt.Sprintf("CREATE %sINDEX %s%s%s ON %s (%s)%s", unique, conc, ine, name, target, col, where), "M4.generated_shape", true
	case n < 6 && len(g.indexes) > 0:
		var names []string
		for k := range g.indexes {
			names = append(names, k)
		}
		slices.Sort(names)
		return fmt.Sprintf("CREATE %sINDEX %s%s%s ON %s (%s)", unique, conc, ine, pick(g, names), target, col), "M4.name_taken", true
	default:
		name := g.name("ix_")
		g.indexes[name] = target
		return fmt.Sprintf("CREATE %sINDEX %s%s%s ON %s (%s)%s", unique, conc, ine, name, target, col, where), tag, true
	}
}

func genAlterTable(g *gen) (string, string, bool) {
	t := g.table()
	if t == nil {
		return "", "", false
	}
	only := ""
	if g.chance(0.1) {
		only = "ONLY "
	}
	ifExists := ""
	if g.chance(0.1) {
		ifExists = "IF EXISTS "
	}
	name := t.name
	if g.chance(0.04) {
		name = g.name("t")
		g.pending = append(g.pending, name)
		return fmt.Sprintf("ALTER TABLE %s ADD COLUMN z int", name), "M5a.forward_table", true
	}
	action, tag := g.alterAction(t)
	if action == "" {
		return "", "", false
	}
	if g.chance(0.1) {
		if a2, t2 := g.alterAction(t); a2 != "" && !strings.HasPrefix(a2, "RENAME") && !strings.HasPrefix(action, "RENAME") && !strings.HasPrefix(a2, "SET SCHEMA") && !strings.HasPrefix(action, "SET SCHEMA") {
			action += ", " + a2
			tag += "+" + t2
		}
	}
	return fmt.Sprintf("ALTER TABLE %s%s%s %s", ifExists, only, name, action), tag, true
}

func (g *gen) col(t *gTable) *gCol { return pick(g, t.cols) }

func (g *gen) alterAction(t *gTable) (string, string) {
	c := g.col(t)
	switch g.r.IntN(32) {
	case 0, 1, 2:
		nc := &gCol{name: g.name("c"), typ: g.colType()}
		t.cols = append(t.cols, nc)
		ine := ""
		if g.chance(0.2) {
			ine = "IF NOT EXISTS "
		}
		return "ADD COLUMN " + ine + g.columnDef(t, nc, true), "M5a.add"
	case 3:
		return "ADD COLUMN " + c.name + " int", "M5a.exists"
	case 4:
		return "ADD COLUMN IF NOT EXISTS " + c.name + " int", "M5a.if_not_exists"
	case 5, 6:
		if len(t.cols) < 2 {
			return "", ""
		}
		cascade := ""
		if g.chance(0.3) {
			cascade = " CASCADE"
		}
		t.cols = slices.DeleteFunc(t.cols, func(x *gCol) bool { return x == c })
		if slices.Contains(t.pk, c.name) {
			t.pk = nil
		}
		return "DROP COLUMN " + c.name + cascade, "M5b.drop"
	case 7:
		return "DROP COLUMN " + pick(g, []string{"nosuch", "IF EXISTS nosuch"}), "M5b.missing"
	case 8, 9:
		typ := pick(g, []string{"bigint", "text", "varchar(80)", "int", "numeric", "int8", "integer"})
		c.typ = typ
		using := ""
		if g.chance(0.2) {
			using = " USING " + c.name + "::" + typ
		}
		return "ALTER COLUMN " + c.name + " TYPE " + typ + using, "M5c.type"
	case 10:
		c.notNull = true
		return "ALTER COLUMN " + c.name + " SET NOT NULL", "M5d.set_not_null"
	case 11:
		c.notNull = false
		return "ALTER COLUMN " + c.name + " DROP NOT NULL", "M5d.drop_not_null"
	case 12:
		return "ALTER COLUMN " + pick(g, []string{c.name, "nosuch"}) + " SET DEFAULT " + pick(g, []string{"0", "'x'", "now()", "NULL"}), "M5e.set_default"
	case 13:
		return "ALTER COLUMN " + c.name + " DROP DEFAULT", "M5e.drop_default"
	case 14, 15:
		cons := ""
		if g.chance(0.6) {
			cn := t.name + pick(g, []string{"_pkey", "_pk", "_" + c.name + "_key", "_" + c.name + "_uq", "_" + c.name + "_fkey", "_" + c.name + "_check", "_c"})
			cons = "CONSTRAINT " + cn + " "
			t.cons = append(t.cons, cn)
		}
		switch g.r.IntN(4) {
		case 0:
			t.pk = []string{c.name}
			return "ADD " + cons + "PRIMARY KEY (" + c.name + ")", "M5f.primary_key"
		case 1:
			t.uniques = append(t.uniques, []string{c.name})
			return "ADD " + cons + "UNIQUE (" + c.name + ")", "M5f.unique"
		case 2:
			t.fks++
			return "ADD " + cons + "FOREIGN KEY (" + c.name + ") " + g.reference(t, c, false), "M5f.foreign_key"
		default:
			return "ADD " + cons + "CHECK (" + c.name + " IS NOT NULL)", "M5f.check"
		}
	case 16, 17, 18:
		ifExists := ""
		if g.chance(0.4) {
			ifExists = "IF EXISTS "
		}
		cascade := ""
		if g.chance(0.3) {
			cascade = " CASCADE"
		}
		var name, tag string
		switch {
		case len(t.cons) > 0 && g.chance(0.6):
			name, tag = pick(g, t.cons), "M5g.written"
			t.cons = slices.DeleteFunc(t.cons, func(s string) bool { return s == name })
		case g.chance(0.6):
			name, tag = t.name+pick(g, []string{"_pkey", "_" + c.name + "_key", "_" + c.name + "_fkey", "_" + c.name + "_check", "_id_fkey"}), "M5g.generated"
		default:
			name, tag = "nosuch_constraint", "M5g.missing"
		}
		if strings.HasSuffix(name, "_pkey") {
			t.pk = nil
		}
		return "DROP CONSTRAINT " + ifExists + name + cascade, tag
	case 19:
		old := c.name
		c.name = pick(g, []string{g.name("r"), t.cols[0].name})
		return "RENAME COLUMN " + old + " TO " + c.name, "M5h.rename_column"
	case 20:
		old := t.name
		t.name = pick(g, []string{g.name("t"), g.name("t"), g.tables[0].name})
		g.dropped = append(g.dropped, old)
		return "RENAME TO " + t.name, "M5i.rename"
	case 21:
		if !g.h.fresh || len(g.schemas) == 0 {
			return "", ""
		}
		return "SET SCHEMA " + pick(g, g.schemas), "M5j.set_schema"
	default:
		return g.unmodelledAction(t, c)
	}
}

// unmodelledAction writes an alter_table_cmd word the pass does not model
// (M5k), with refused variants for A0's latent claims, or an unknown
// action (D).
func (g *gen) unmodelledAction(t *gTable, c *gCol) (string, string) {
	switch g.r.IntN(22) {
	case 0:
		return "OWNER TO gv_oracle_role", "M5k.owner"
	case 1:
		return "ENABLE TRIGGER " + pick(g, []string{"nosuch_trigger", "ALL", "USER"}), "M5k.enable_trigger"
	case 2:
		return "DISABLE TRIGGER " + pick(g, []string{"nosuch_trigger", "ALL"}), "M5k.disable_trigger"
	case 3:
		return "SET (fillfactor = 70)", "M5k.set_storage"
	case 4:
		return "RESET (fillfactor)", "M5k.reset"
	case 5:
		var ix []string
		for k, v := range g.indexes {
			if v == t.name {
				ix = append(ix, k)
			}
		}
		slices.Sort(ix)
		ix = append(ix, "nosuch_index", t.name+"_pkey")
		return "CLUSTER ON " + pick(g, ix), "M5k.cluster_on"
	case 6:
		return "SET WITHOUT CLUSTER", "M5k.set_without_cluster"
	case 7:
		if p := g.table(); p != nil {
			return "INHERIT " + p.name, "M5k.inherit"
		}
	case 8:
		if p := g.table(); p != nil {
			return "NO INHERIT " + p.name, "M5k.no_inherit"
		}
	case 9:
		return "VALIDATE CONSTRAINT " + pick(g, append(slices.Clone(t.cons), "nosuch_constraint")), "M5k.validate"
	case 10:
		return "ALTER CONSTRAINT " + pick(g, append(slices.Clone(t.cons), "nosuch_constraint")) + " DEFERRABLE", "M5k.alter_constraint"
	case 11:
		from := pick(g, append(slices.Clone(t.cons), "nosuch_constraint"))
		to := g.name("k")
		for i, n := range t.cons {
			if n == from {
				t.cons[i] = to
			}
		}
		return "RENAME CONSTRAINT " + from + " TO " + to, "M5k.rename_constraint"
	case 12:
		return "REPLICA IDENTITY FULL", "M5k.replica_identity"
	case 13:
		return pick(g, []string{"ENABLE ROW LEVEL SECURITY", "FORCE ROW LEVEL SECURITY", "NO FORCE ROW LEVEL SECURITY"}), "M5k.row_security"
	case 14:
		return pick(g, []string{"SET LOGGED", "SET UNLOGGED"}), "M5k.set_logged"
	case 15:
		return "NOT OF", "M5k.not_of"
	case 16:
		return "ALTER COLUMN " + c.name + " SET STATISTICS 100", "M5k.alter_column_other"
	case 17:
		return "ALTER COLUMN " + c.name + " ADD GENERATED ALWAYS AS IDENTITY", "M5k.add_identity"
	case 18:
		return "SET ACCESS METHOD heap", "D.set_access_method"
	case 19:
		return "FROBNICATE " + c.name, "D.unknown_action"
	case 20:
		return "ENABLE RULE nosuch_rule", "M5k.enable_rule"
	default:
		if p := g.table(); p != nil {
			return "DETACH PARTITION " + p.name, "M5k.detach"
		}
	}
	return "SET WITHOUT OIDS", "M5k.set_without_oids"
}

func genDropTable(g *gen) (string, string, bool) {
	ifExists := ""
	if g.chance(0.3) {
		ifExists = "IF EXISTS "
	}
	cascade := ""
	if g.chance(0.3) {
		cascade = " CASCADE"
	}
	switch {
	case g.chance(0.15):
		return "DROP TABLE " + ifExists + pick(g, []string{"nosuch_table", "ext_b"}) + cascade, "M6.missing_or_outside", true
	case len(g.tables) == 0:
		return "", "", false
	}
	t := g.table()
	g.tables = slices.DeleteFunc(g.tables, func(x *gTable) bool { return x == t })
	g.dropped = append(g.dropped, t.name)
	return "DROP TABLE " + ifExists + t.name + cascade, "M6.drop", true
}

func genDropIndex(g *gen) (string, string, bool) {
	ifExists := ""
	if g.chance(0.3) {
		ifExists = "IF EXISTS "
	}
	conc := ""
	tag := "M7.drop"
	if g.chance(0.1) {
		conc, tag = "CONCURRENTLY ", "M7.concurrently_tx"
	}
	var names []string
	for k := range g.indexes {
		names = append(names, k)
	}
	slices.Sort(names)
	if t := g.table(); t != nil {
		names = append(names, t.name+"_pkey", t.name+"_"+g.col(t).name+"_idx", t.name+"_"+g.col(t).name+"_key")
	}
	names = append(names, "nosuch_index")
	name := pick(g, names)
	delete(g.indexes, name)
	return "DROP INDEX " + conc + ifExists + name, tag, true
}

func genAlterIndex(g *gen) (string, string, bool) {
	var names []string
	for k := range g.indexes {
		names = append(names, k)
	}
	slices.Sort(names)
	if t := g.table(); t != nil {
		names = append(names, t.name+"_pkey", t.name)
	}
	if len(names) == 0 {
		return "", "", false
	}
	name := pick(g, names)
	to := g.name("ix_")
	if g.chance(0.2) && len(names) > 1 {
		to = pick(g, names)
	}
	if tbl, ok := g.indexes[name]; ok {
		delete(g.indexes, name)
		g.indexes[to] = tbl
	}
	ifExists := ""
	if g.chance(0.2) {
		ifExists = "IF EXISTS "
	}
	if g.chance(0.15) {
		return "ALTER INDEX " + ifExists + name + " SET (fillfactor = 80)", "R4.alter_index_other", true
	}
	return "ALTER INDEX " + ifExists + name + " RENAME TO " + to, "M8.rename", true
}

func genRelation(g *gen) (string, string, bool) {
	t := g.table()
	switch g.r.IntN(7) {
	case 0, 1:
		if t == nil {
			return "", "", false
		}
		name := g.name("v")
		cols := "*"
		tag := "R1.view"
		if g.chance(0.3) {
			cols = pick(g, append([]string{"nosuch"}, names2(t)...))
			tag = "R1.view_columns"
		}
		orReplace := ""
		if g.chance(0.2) && len(g.views) > 0 {
			orReplace, name = "OR REPLACE ", pick(g, g.views)
			tag = "R1.or_replace_view"
		}
		g.views = append(g.views, name)
		return fmt.Sprintf("CREATE %sVIEW %s AS SELECT %s FROM %s", orReplace, name, cols, t.name), tag, true
	case 2:
		if t == nil {
			return "", "", false
		}
		name := g.name("mv")
		g.matviews = append(g.matviews, name)
		return fmt.Sprintf("CREATE MATERIALIZED VIEW %s AS SELECT * FROM %s", name, t.name), "R1.materialized_view", true
	case 3:
		name := g.name("s")
		g.seqs = append(g.seqs, name)
		return "CREATE SEQUENCE " + name, "R1.sequence", true
	case 4:
		name := g.name("ct")
		g.views = append(g.views, name)
		return "CREATE TYPE " + name + " AS (a int, b text)", "R1.composite_type", true
	case 5:
		name := g.name("d")
		g.domains = append(g.domains, name)
		return "CREATE DOMAIN " + name + " AS int CHECK (VALUE >= 0)", "R1.domain", true
	default:
		name := g.name("tmp")
		return "CREATE TEMP TABLE " + name + " (id int)", "R1.temp_table", true
	}
}

func names2(t *gTable) []string {
	var out []string
	for _, c := range t.cols {
		out = append(out, c.name)
	}
	return out
}

func genEnum(g *gen) (string, string, bool) {
	if len(g.enums) == 0 || g.chance(0.3) {
		name := g.name("e")
		if len(g.pendingEnums) > 0 && g.chance(0.6) {
			name, g.pendingEnums = g.pendingEnums[0], g.pendingEnums[1:]
		}
		e := &gEnum{name: name, labels: []string{"a", "b"}}
		if g.chance(0.5) {
			e.labels = append(e.labels, "c")
		}
		g.enums = append(g.enums, e)
		var q []string
		for _, l := range e.labels {
			q = append(q, "'"+l+"'")
		}
		return fmt.Sprintf("CREATE TYPE %s AS ENUM (%s)", name, strings.Join(q, ", ")), "R2.create_enum", true
	}
	e := pick(g, g.enums)
	switch n := g.r.IntN(10); {
	case n < 3:
		label := fmt.Sprintf("v%d", g.r.IntN(4))
		ine := ""
		if g.chance(0.3) {
			ine = "IF NOT EXISTS "
		}
		pos := ""
		if g.chance(0.2) {
			pos = " BEFORE '" + pick(g, append(slices.Clone(e.labels), "nosuch")) + "'"
		}
		tag := "R3.add_value"
		if slices.Contains(e.labels, label) {
			tag = "R3.add_value_existing"
		} else {
			e.labels = append(e.labels, label)
			g.enumAdded = append(g.enumAdded, label)
		}
		return fmt.Sprintf("ALTER TYPE %s ADD VALUE %s'%s'%s", e.name, ine, label, pos), tag, true
	case n < 5:
		return fmt.Sprintf("ALTER TYPE %s ADD VALUE '%s'", e.name, pick(g, e.labels)), "R3.add_value_existing", true
	case n < 6:
		// A type the set creates later (K2, K1 as a class); if it never
		// does, the prelude creates it (E1).
		name := g.name("e")
		g.pendingEnums = append(g.pendingEnums, name)
		return fmt.Sprintf("ALTER TYPE %s ADD VALUE 'z'", name), "R3.missing_type", true
	case n < 7:
		old := pick(g, e.labels)
		to := fmt.Sprintf("r%d", g.r.IntN(3))
		for i, l := range e.labels {
			if l == old {
				e.labels[i] = to
			}
		}
		return fmt.Sprintf("ALTER TYPE %s RENAME VALUE '%s' TO '%s'", e.name, old, to), "R3.rename_value", true
	case n < 8 && len(g.enumAdded) > 0:
		return fmt.Sprintf("SELECT '%s'::%s", pick(g, g.enumAdded), e.name), "R3.unsafe_use", true
	case n < 9:
		old := e.name
		e.name = g.name("e")
		return fmt.Sprintf("ALTER TYPE %s RENAME TO %s", old, e.name), "R4.rename_type", true
	default:
		cascade := ""
		if g.chance(0.4) {
			cascade = " CASCADE"
		}
		g.enums = slices.DeleteFunc(g.enums, func(x *gEnum) bool { return x == e })
		return "DROP TYPE " + e.name + cascade, "R5.drop_type", true
	}
}

func genOtherAlter(g *gen) (string, string, bool) {
	switch g.r.IntN(4) {
	case 0:
		if len(g.views) == 0 {
			return "", "", false
		}
		v := pick(g, g.views)
		return "ALTER VIEW " + v + " RENAME TO " + g.name("v"), "R4.rename_view", true
	case 1:
		if len(g.seqs) == 0 {
			return "", "", false
		}
		return "ALTER SEQUENCE " + pick(g, g.seqs) + " RESTART WITH 10", "N1.alter_sequence", true
	case 2:
		if len(g.domains) == 0 {
			return "", "", false
		}
		return "ALTER DOMAIN " + pick(g, g.domains) + " SET DEFAULT 1", "R4.alter_domain", true
	default:
		if len(g.funcs) == 0 {
			return "", "", false
		}
		f := pick(g, g.funcs)
		to := g.name("f")
		g.funcs = append(g.funcs, to)
		return "ALTER FUNCTION " + f + "() RENAME TO " + to, "R6.rename_function", true
	}
}

func genOtherDrop(g *gen) (string, string, bool) {
	cascade := ""
	if g.chance(0.3) {
		cascade = " CASCADE"
	}
	ifExists := ""
	if g.chance(0.3) {
		ifExists = "IF EXISTS "
	}
	switch g.r.IntN(5) {
	case 0:
		if len(g.views) == 0 {
			return "", "", false
		}
		return "DROP VIEW " + ifExists + pick(g, g.views) + cascade, "R5.drop_view", true
	case 1:
		if len(g.seqs) == 0 {
			return "", "", false
		}
		return "DROP SEQUENCE " + ifExists + pick(g, g.seqs) + cascade, "R5.drop_sequence", true
	case 2:
		if len(g.domains) == 0 {
			return "", "", false
		}
		return "DROP DOMAIN " + ifExists + pick(g, g.domains) + cascade, "R5.drop_domain", true
	case 3:
		if len(g.funcs) == 0 {
			return "", "", false
		}
		return "DROP FUNCTION " + ifExists + pick(g, g.funcs) + "()" + cascade, "R5.drop_function", true
	default:
		return "DROP " + pick(g, []string{"TRIGGER IF EXISTS nosuch ON ext_a", "POLICY IF EXISTS nosuch ON ext_a", "COLLATION IF EXISTS nosuch", "OPERATOR IF EXISTS ===(int, int)"}), "R5.drop_other", true
	}
}

func genFunction(g *gen) (string, string, bool) {
	switch g.r.IntN(4) {
	case 0:
		name := g.name("f")
		g.funcs = append(g.funcs, name)
		return "CREATE FUNCTION " + name + "() RETURNS int LANGUAGE sql AS $$ SELECT 1 $$", "R6.function_sql", true
	case 1:
		t := g.table()
		if t == nil {
			return "", "", false
		}
		name := g.name("f")
		g.funcs = append(g.funcs, name)
		col := pick(g, append(names2(t), "nosuch"))
		return fmt.Sprintf("CREATE FUNCTION %s() RETURNS bigint LANGUAGE sql AS $$ SELECT count(%s) FROM %s $$", name, col, t.name), "R6.function_sql_body", true
	case 2:
		name := g.name("tf")
		g.trigfns = append(g.trigfns, name)
		return "CREATE FUNCTION " + name + "() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$", "R6.trigger_function", true
	default:
		name := g.name("p")
		g.funcs = append(g.funcs, name)
		return "CREATE PROCEDURE " + name + "() LANGUAGE sql AS $$ SELECT 1 $$", "R6.procedure", true
	}
}

func genNoEffect(g *gen) (string, string, bool) {
	t := g.table()
	target := "ext_a"
	if t != nil {
		target = t.name
	}
	if len(g.dropped) > 0 && g.chance(0.1) {
		target = pick(g, g.dropped)
	}
	col := "id"
	if t != nil {
		col = g.col(t).name
		if g.chance(0.15) {
			col = "nosuch"
		}
	}
	switch g.r.IntN(12) {
	case 0:
		return "COMMENT ON TABLE " + target + " IS 'table'", "N1.comment_table", true
	case 1, 2:
		return "COMMENT ON COLUMN " + target + "." + col + " IS 'column'", "N1.comment_column", true
	case 3:
		return "GRANT SELECT ON " + target + " TO gv_oracle_role", "N1.grant", true
	case 4:
		return "REVOKE ALL ON " + target + " FROM gv_oracle_role", "N1.revoke", true
	case 5:
		fn := "nosuch_fn"
		if len(g.trigfns) > 0 {
			fn = pick(g, g.trigfns)
		}
		return fmt.Sprintf("CREATE TRIGGER %s BEFORE INSERT ON %s FOR EACH ROW EXECUTE FUNCTION %s()", g.name("trg"), target, fn), "N1.create_trigger", true
	case 6:
		return fmt.Sprintf("CREATE POLICY %s ON %s USING (true)", pick(g, []string{g.name("pol"), "pol"}), target), "N1.create_policy", true
	case 7:
		return fmt.Sprintf("CREATE STATISTICS %s ON id, %s FROM %s", g.name("st"), col, target), "N1.create_statistics", true
	case 8:
		return "LOCK TABLE " + target + " IN SHARE MODE", "N1.lock", true
	case 9:
		return "NOTIFY gv_channel", "N1.notify", true
	case 10:
		return "SECURITY LABEL ON TABLE " + target + " IS 'x'", "N1.security_label", true
	default:
		return "COMMENT ON INDEX " + pick(g, []string{"nosuch_index", target + "_pkey"}) + " IS 'index'", "N1.comment_index", true
	}
}

func genSession(g *gen) (string, string, bool) {
	switch g.r.IntN(7) {
	case 0:
		return "SET lock_timeout = '5s'", "N2.set", true
	case 1:
		return "RESET lock_timeout", "N2.reset", true
	case 2:
		return "SET LOCAL statement_timeout = 0", "N2.set_local", true
	case 3:
		return pick(g, []string{"BEGIN", "START TRANSACTION"}), "N2.begin", true
	case 4:
		return pick(g, []string{"COMMIT", "END"}), "N2.commit", true
	case 5:
		return "DISCARD PLANS", "N2.discard", true
	default:
		return "SET client_min_messages = warning", "N2.set", true
	}
}

func genData(g *gen) (string, string, bool) {
	t := g.table()
	if t == nil {
		return "SELECT 1", "N3.select", true
	}
	switch g.r.IntN(10) {
	case 0, 1:
		return "INSERT INTO " + t.name + " DEFAULT VALUES", "N3.insert", true
	case 2:
		return "UPDATE " + t.name + " SET id = id", "N3.update", true
	case 3:
		return "DELETE FROM " + t.name, "N3.delete", true
	case 4:
		return "ANALYZE " + t.name, "N3.analyze", true
	case 5:
		return "REINDEX TABLE " + t.name, "N3.reindex", true
	case 6:
		return "CLUSTER " + t.name, "N3.cluster", true
	case 7:
		return "TRUNCATE " + t.name, "N3.truncate", true
	case 8:
		if len(g.matviews) > 0 {
			return "REFRESH MATERIALIZED VIEW " + pick(g, g.matviews), "N3.refresh", true
		}
		return "VALUES (1)", "N3.values", true
	default:
		return "SELECT count(*) FROM " + t.name, "N3.select", true
	}
}

func genOpaque(g *gen) (string, string, bool) {
	switch g.r.IntN(6) {
	case 0:
		return "DO $$ BEGIN PERFORM 1; END $$", "O1.do", true
	case 1:
		if t := g.table(); t != nil {
			c := g.name("c")
			t.cols = append(t.cols, &gCol{name: c, typ: "int"})
			return fmt.Sprintf("DO $$ BEGIN EXECUTE 'ALTER TABLE %s ADD COLUMN %s int'; END $$", t.name, c), "O1.do_ddl", true
		}
	case 2:
		if len(g.funcs) > 0 {
			return "SELECT " + pick(g, g.funcs) + "()", "O1.function_call", true
		}
	case 3:
		if !g.h.fresh {
			return "", "", false
		}
		return "CREATE EXTENSION IF NOT EXISTS " + pick(g, []string{"citext", "pg_trgm", "hstore", "nosuch_extension"}), "O2.extension", true
	case 4:
		return pick(g, []string{"SAVEPOINT sp1", "RELEASE SAVEPOINT sp1", "ROLLBACK"}), "O5.transaction", true
	default:
		return "SET standard_conforming_strings = off", "O4.lexing", true
	}
	return "", "", false
}

func genFailing(g *gen) (string, string, bool) {
	t := g.table()
	if t == nil {
		return "VACUUM", "F1.vacuum", true
	}
	switch g.r.IntN(3) {
	case 0:
		return "VACUUM " + t.name, "F1.vacuum", true
	case 1:
		return "REINDEX TABLE CONCURRENTLY " + t.name, "F1.reindex_concurrently", true
	default:
		return "CREATE TEMP TABLE public.tmp_x (id int)", "F1.temp_in_schema", true
	}
}

func genDefault(g *gen) (string, string, bool) {
	switch g.r.IntN(6) {
	case 0:
		return "SELEC 1", "D.unreadable", true
	case 1:
		if t := g.table(); t != nil {
			return "CREATE RULE " + g.name("rule") + " AS ON INSERT TO " + t.name + " DO INSTEAD NOTHING", "D.create_rule", true
		}
	case 2:
		return "CREATE CONVERSION " + g.name("conv") + " FOR 'LATIN1' TO 'UTF8' FROM iso8859_1_to_utf8", "D.create_conversion", true
	case 3:
		return "IMPORT FOREIGN SCHEMA nosuch FROM SERVER nosuch INTO public", "D.import_foreign_schema", true
	case 4:
		return "CREATE TEXT SEARCH CONFIGURATION " + g.name("ts") + " (COPY = simple)", "D.text_search", true
	default:
		if t := g.table(); t != nil {
			return "ALTER TABLE " + t.name + " ALTER COLUMN " + g.col(t).name + " SET COMPRESSION pglz", "M5k.alter_column_other", true
		}
	}
	return "", "", false
}

func genSchema(g *gen) (string, string, bool) {
	if !g.h.fresh {
		return "", "", false
	}
	switch g.r.IntN(5) {
	case 0, 1:
		s := g.name("sc")
		g.schemas = append(g.schemas, s)
		return "CREATE SCHEMA " + s, "R1.create_schema", true
	case 2:
		if len(g.schemas) == 0 {
			return "", "", false
		}
		from := pick(g, g.schemas)
		to := g.name("sc")
		g.schemas = slices.DeleteFunc(g.schemas, func(s string) bool { return s == from })
		g.schemas = append(g.schemas, to)
		return "ALTER SCHEMA " + from + " RENAME TO " + to, "M9.rename_schema", true
	case 3:
		if len(g.schemas) == 0 {
			return "", "", false
		}
		return "SET search_path TO " + pick(g, g.schemas) + ", public", "O3.search_path", true
	default:
		if len(g.schemas) == 0 {
			return "", "", false
		}
		s := pick(g, g.schemas)
		return fmt.Sprintf("CREATE TABLE %s.%s (id int PRIMARY KEY)", s, g.name("q")), "M1.qualified", true
	}
}

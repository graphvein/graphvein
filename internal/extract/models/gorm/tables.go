package gorm

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// rows names this contract's rows for the kit's shared steps: the model
// table (GO-M1) for the node shapes, GO-R3 for foreign-key resolution.
// The migration comparison has no GORM row (no tag), as for Prisma,
// Drizzle and Sequelize.
var rows = models.Rows{Emit: "GO-M1", ForeignKeys: "GO-R3"}

// faultHook is a test-only hook called at the start of every isolated
// step.
var faultHook func(step, subject string)

// Diagnostic code of a model whose table is not claimed (the server marks
// the group's table list may_be_incomplete).
const codeNotClaimed = "schema_model_not_claimed"

// Link discovers every GORM module of the repository, claims the tables
// and columns of the models its migrate sites list under the claim
// contract (docs/extract/schema-gorm.md) and emits them through the model
// kit, one group per module (models/gorm/<module dir>). A failure in one
// module or table never stops the others: it becomes a diagnostic.
//
// Step GB3 claims rows GO-K4, GO-M1, GO-F1, GO-F2, GO-T1, GO-T2, GO-C1-C4,
// GO-I1 and the provable failures a single struct shows (GO-P1); step GB4
// adds relations (GO-R1-R3), the auto-added tables and the migrate order,
// and the indexer call.
func Link(in Input) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	mods, diags := Discover(in)
	steps := &models.Steps{Hook: faultHook}
	var nodes []contract.Node
	var edges []contract.Edge
	reached := map[string]bool{}
	for _, m := range mods {
		if m.Refused != "" || m.res == nil || len(m.Dialects) == 0 {
			continue
		}
		b := &builder{in: in, m: m, steps: steps}
		steps.Guard(m.GoMod, "group", "GORM module "+m.Dir, b.build)
		b.reached(reached)
		if len(b.tables) == 0 {
			continue
		}
		n, e := models.Link(b.tables, models.LinkInput{Group: b.group(), Rows: rows, Tag: in.Tag, Steps: steps, Sources: in.Sources, Migrations: in.Migrations, Edges: in.Edges})
		nodes = append(nodes, n...)
		edges = append(edges, e...)
	}
	// GO-M2: a struct GO-K3's auto-add reaches is a model.
	kept := diags[:0]
	for _, d := range diags {
		if d.Code != codeNotProven || !reached[d.Message] {
			kept = append(kept, d)
		}
	}
	diags = append(kept, steps.Diagnostics...)
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Identity < nodes[j].Identity })
	return nodes, edges, diags
}

// builder builds one module's tables.
type builder struct {
	in     Input
	m      *Module
	steps  *models.Steps
	p      *porter
	tables []*models.Table
	// idx and idxWhy are each schema's indexes and why its index list is
	// not known; w is the migrate simulation.
	idx    map[*gschema][]*gindex
	idxWhy map[*gschema]string
	w      *world
}

func (b *builder) tag(row string) {
	if b.in.Tag != nil {
		b.in.Tag(row)
	}
}

// verificationOf is a dialect's verification: postgres and sqlite facts
// were compared by the investigation oracle, mysql facts are mapped from the
// driver source (GO-V5).
func verificationOf(d string) string {
	if d == DialectMySQL {
		return models.VerificationMapped
	}
	return models.VerificationOracle
}

// group is the module's kit group: one dialect, or the run-time dialects
// (GO-V3, Revision 1 O1) whose dialect-dependent facts are per dialect.
func (b *builder) group() models.Group {
	m := b.m
	g := models.Group{Tool: Tool, Root: m.Dir, Pins: m.Pins, Assumptions: []string{"GO-V1", "GO-V2", "GO-V3", "GO-V4", "GO-V6", "GO-V7"}, Verification: models.VerificationOracle}
	if len(m.Dialects) == 1 {
		g.Dialect = m.Dialects[0]
		g.Verification = verificationOf(g.Dialect)
	} else {
		g.Dialects = append([]string(nil), m.Dialects...)
	}
	for _, d := range m.Dialects {
		if d == DialectMySQL {
			g.Assumptions = append(g.Assumptions, "GO-V5")
			sort.Strings(g.Assumptions)
		}
	}
	return g
}

// notClaimed reports a model whose table is not claimed: the group's table
// list may be incomplete.
func (b *builder) notClaimed(row, file string, line int, format string, args ...any) {
	b.tag(row)
	msg := fmt.Sprintf("%s:%d: ", file, line) + fmt.Sprintf(format, args...) + ListMarker(b.m.SetKey())
	b.steps.Diag(file, "info", codeNotClaimed, msg)
}

// build parses the module's models, simulates its migrate calls
// (migrate.go) and builds the tables it may claim: the listed models, the
// tables AutoMigrate adds (GO-K3) and the join tables (GO-R2).
func (b *builder) build() {
	m := b.m
	if !m.Config.Naming.Known {
		b.tag("GO-K4")
		for _, mo := range m.Models {
			b.notClaimed("GO-K4", mo.File, mo.Struct.Line, "model %s: the table and column names are unknown: %s", mo.Struct.Name, m.Config.Naming.Reason)
		}
		return
	}
	b.p = &porter{r: m.res, namer: newNamer(m.Config.Naming.Naming, m.Dialects[0]), cache: map[string]*gschema{}}
	for _, d := range m.Dialects {
		if d == DialectPostgres {
			b.p.limitName = 63
		}
	}
	if len(m.InflectionChanged) > 0 {
		b.p.inflection = "the module changes inflection's rules (" + strings.Join(m.InflectionChanged, ", ") + ")"
	}
	byModel := map[string]*gschema{}
	var all []*gschema
	seen := map[*gschema]bool{}
	for _, mo := range m.Models {
		mo := mo
		b.steps.Guard(mo.File, "parse", "GORM model "+mo.Identity(), func() {
			sc := b.p.parse(&structRef{File: mo.File, Dir: mo.Dir, Struct: mo.Struct}, false, "", mo.Struct.Line, 0)
			byModel[mo.Identity()] = sc
			reach := map[*gschema]bool{}
			closureOf(sc, reach)
			var list []*gschema
			for x := range reach {
				if !seen[x] {
					seen[x] = true
					list = append(list, x)
				}
			}
			sort.Slice(list, func(i, j int) bool { return schemaKey(list[i]) < schemaKey(list[j]) })
			all = append(all, list...)
		})
	}
	// Indexes per schema (GO-I1).
	b.idx, b.idxWhy = map[*gschema][]*gindex{}, map[*gschema]string{}
	for _, sc := range all {
		b.idx[sc], b.idxWhy[sc] = b.indexes(sc)
	}
	// The migrate calls (GO-K3, GO-P1).
	w := b.newWorld(byModel)
	w.run()
	w.crossCalls()
	b.w = w
	// Two schemas with one table name that are not one join table: a
	// later AutoMigrate alters the first's table (not ported, row D).
	clash := map[string]bool{}
	for _, t := range w.tables {
		if len(t.schemas) < 2 {
			continue
		}
		for _, sc := range t.schemas {
			if !sc.Join {
				clash[t.name] = true
			}
		}
	}
	joins := map[string]int{}
	for _, sc := range all {
		for _, r := range sc.Rels {
			if r.Join != nil && r.Join.Table != "" {
				joins[r.Join.Table]++
			}
		}
	}
	for _, t := range w.order {
		sc := t.schemas[0]
		ref := b.tableRef(sc)
		switch {
		case t.failed != "":
			continue
		case sc.Table == "":
			b.notClaimed("GO-K4", ref.file, ref.line, "model %s: the table name is unknown: %s", schemaName(sc), sc.TableWhy)
			continue
		case b.p.limitName > 0 && len(sc.Table) > b.p.limitName:
			b.notClaimed("GO-K4", ref.file, ref.line, "model %s: the table name %s is longer than 63 bytes (postgres truncates it)", schemaName(sc), sc.Table)
			continue
		case sc.Join && b.joinReplaced(sc) != "":
			b.notClaimed("D", ref.file, ref.line, "join table %s is not claimed: %s", sc.Table, b.joinReplaced(sc))
			continue
		case clash[t.name]:
			for _, x := range t.schemas {
				r := b.tableRef(x)
				b.notClaimed("D", r.file, r.line, "model %s: %d models of the group have the table name %s; a later AutoMigrate alters the first table (not read)", schemaName(x), len(t.schemas), sc.Table)
			}
			continue
		}
		t := t
		b.steps.Guard(ref.file, "table", "GORM model "+ref.identity, func() {
			if tb := b.table(t, ref, sc, joins[sc.Table] > 1); tb != nil {
				b.tables = append(b.tables, tb)
			}
		})
	}
	// Tables the calls do not create: a failure (GO-P1), or what may fail.
	var names []string
	for n := range w.tables {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		t := w.tables[n]
		if t.created && t.failed == "" {
			continue
		}
		sc := t.schemas[0]
		ref := b.tableRef(sc)
		why := t.failed
		if why == "" {
			why = t.withheld
		}
		if why == "" {
			continue
		}
		b.notClaimed(t.withheldRow, ref.file, ref.line, "model %s is not claimed: %s", schemaName(sc), why)
	}
	// Relations the port does not read (row D): a polymorphic relation
	// adds no table; an embedded struct's relation may (a belongs-to
	// target).
	for _, t := range w.order {
		for _, sc := range t.schemas {
			for _, r := range sc.Rels {
				if r.Unread == "" {
					continue
				}
				b.tag("D")
				msg := fmt.Sprintf("%s:%d: %s", r.Field.File, r.Field.Line, r.Unread)
				if r.Kind != relPolymorphc {
					msg += ListMarker(m.SetKey())
				}
				b.steps.Diag(r.Field.File, "info", codeUnread, msg)
			}
		}
	}
	for _, n := range sortedKeys(w.maybe) {
		if t := w.tables[n]; t == nil || !t.created {
			b.tag("GO-K3")
			msg := w.maybe[n] + ListMarker(m.SetKey())
			b.steps.Diag(m.GoMod, "info", codeNotClaimed, msg)
		}
	}
}

// joinReplaced says why a join table's schema may not be the one GORM
// builds from the many2many relation: a SetupJoinTable call names its
// relation, or a SetupJoinTable call that is not read may name any
// (Revision 2, A2; row D).
func (b *builder) joinReplaced(sc *gschema) string {
	rel := sc.JoinOf
	for _, j := range b.m.JoinSetups {
		switch {
		case j.Reason != "":
			return fmt.Sprintf("the SetupJoinTable call at %s:%d is not read (%s), so it may replace any join table", j.File, j.Line, j.Reason)
		case rel != nil && rel.Field != nil && rel.Schema != nil && rel.Schema.Ref != nil && !rel.Schema.Ref.Model && rel.Field.Name == j.Field && "table:"+rel.Schema.Ref.File+"#"+rel.Schema.Ref.Struct.Name == j.Owner.Identity():
			join := "a join model that is not read"
			if j.Join != nil {
				join = "the join model " + j.Join.Struct.Name
			}
			return fmt.Sprintf("SetupJoinTable at %s:%d builds it from %s", j.File, j.Line, join)
		}
	}
	return ""
}

// changes applies the Migrator DDL calls that may change the table
// (Revision 2, A3; row GO-K3): the column and constraint sets are partial;
// an index or constraint method, or a table method, also leaves the index
// and constraint lists unknown; a column method leaves the column it names
// unknown (every column when it names none read, or for a table method).
// It returns why the lists are unknown ("" when they are not) and the
// columns (DB or field names; "*" for every column) made unknown.
func (b *builder) changes(t *models.Table, sc *gschema) (string, map[string]string) {
	elements := ""
	cols := map[string]string{}
	for _, ch := range b.m.Changes {
		switch {
		case ch.Model != nil:
			if sc.Join || "table:"+sc.Ref.File+"#"+sc.Ref.Struct.Name != ch.Model.Identity() {
				continue
			}
		case ch.Table != "":
			if ch.Table != sc.Table {
				continue
			}
		}
		b.tag("GO-K3")
		why := fmt.Sprintf("Migrator().%s at %s:%d changes this table after the migrate calls", ch.Method, ch.File, ch.Line)
		if ch.Reason != "" {
			why = fmt.Sprintf("Migrator().%s at %s:%d may change this table after the migrate calls (%s)", ch.Method, ch.File, ch.Line, ch.Reason)
		}
		t.ColumnsComplete, t.ConstraintsComplete = false, false
		t.Reasons = append(t.Reasons, models.Reason{Row: "GO-K3", Fact: models.Fact("column_set"), File: ch.File, Line: ch.Line, Reason: why})
		t.Reasons = append(t.Reasons, models.Reason{Row: "GO-K3", Fact: models.Fact("constraint_set"), File: ch.File, Line: ch.Line, Reason: why})
		if (elementMethods[ch.Method] || tableMethods[ch.Method]) && elements == "" {
			elements = why
		}
		switch {
		case tableMethods[ch.Method], columnMethods[ch.Method] && ch.Column == "":
			cols["*"] = why
		case columnMethods[ch.Method]:
			cols[ch.Column] = why
		}
	}
	return elements, cols
}

// rawChanges applies the statements of Exec and Raw DDL text that may
// change the table (Revision 2 addendum; row GO-K3): an
// ALTER, DROP, RENAME or CREATE TABLE naming the table, or one whose
// target is not read,
// leaves the table only its name and its columns' names (every column
// unknown, as Diesel's non-benign patch removal, ADR 0062): column and
// constraint sets partial, schema, primary key and the index and
// constraint lists unknown. A CREATE INDEX naming the table, or one whose
// table is not read, leaves the index and constraint lists unknown. It
// adds the columns made unknown to cols ("*") and returns why the lists
// are unknown ("" when they are not).
func (b *builder) rawChanges(t *models.Table, sc *gschema, cols map[string]string) string {
	elements := ""
	for _, ch := range b.m.RawChanges {
		if !ch.matches(sc.Table) {
			continue
		}
		b.tag("GO-K3")
		target := "names this table"
		if len(ch.Tables) == 0 {
			target = "may change any table (its target table is not read)"
		}
		var why string
		if ch.IndexOnly {
			why = fmt.Sprintf("%s at %s:%d runs %q, which %s: the index and constraint lists are unknown", ch.Method, ch.File, ch.Line, ch.Statement, target)
		} else {
			why = fmt.Sprintf("%s at %s:%d runs %q, which %s: every fact of the table other than its table and column names is unknown", ch.Method, ch.File, ch.Line, ch.Statement, target)
			t.ColumnsComplete, t.PrimaryKeyOpen = false, true
			t.Schema, t.SchemaKnown = nil, false
			t.Reasons = append(t.Reasons, models.Reason{Row: "GO-K3", Fact: models.Fact("column_set"), File: ch.File, Line: ch.Line, Reason: why})
			if _, ok := cols["*"]; !ok {
				cols["*"] = why
			}
		}
		t.ConstraintsComplete = false
		t.Reasons = append(t.Reasons, models.Reason{Row: "GO-K3", Fact: models.Fact("constraint_set"), File: ch.File, Line: ch.Line, Reason: why})
		if elements == "" {
			elements = why
		}
	}
	return elements
}

// reached adds the GO-M2 markers of the structs the module's migrate
// calls reach (listed, auto-added or failing, and the structs they
// embed), which are therefore not "not proven migrated".
func (b *builder) reached(out map[string]bool) {
	if b.w == nil {
		return
	}
	r := b.m.res
	var embed func(file string, s *facts.GoStruct)
	seen := map[string]bool{}
	embed = func(file string, s *facts.GoStruct) {
		key := file + "#" + s.Name
		if seen[key] {
			return
		}
		seen[key] = true
		out[notProvenMessage(file, s.Line, s.Name)] = true
		for _, f := range s.Fields {
			if f.Embedded {
				if mf, ms := r.structOf(file, f.Type); ms != nil {
					embed(mf, ms)
				}
			}
		}
	}
	for _, t := range b.w.tables {
		for _, sc := range t.schemas {
			if !sc.Join && !sc.Ref.Model {
				embed(sc.Ref.File, sc.Ref.Struct)
			}
		}
	}
}

// schemaKey orders schemas: the struct's file and name, a join table
// after its owner.
func schemaKey(s *gschema) string {
	return s.Ref.key()
}

// tableRef is where a table's record points: a model struct, or a join
// table's many2many field (identity table:<file>#<Struct>.<Field>, row
// GO-R2).
type tableRef struct {
	identity, file, model string
	line, endLine         int
	// structName is the struct whose node contains the table ("" for a
	// join table).
	structName string
}

func (b *builder) tableRef(sc *gschema) tableRef {
	st := sc.Ref.Struct
	r := tableRef{identity: "table:" + sc.Ref.File + "#" + st.Name, file: sc.Ref.File, model: st.Name, line: st.Line, endLine: st.EndLine, structName: st.Name}
	if sc.Join {
		r.structName = ""
	}
	return r
}

func (b *builder) has(d string) bool {
	for _, x := range b.m.Dialects {
		if x == d {
			return true
		}
	}
	return false
}

// dialectFailure is a provable failure of a struct on a dialect of the
// group (GO-P1). sqlite: two autoincrement integer fields (each is written
// "integer PRIMARY KEY AUTOINCREMENT"), an index with a type or class, or
// a raw default that is a function call not wrapped in parentheses
// (SQLite accepts only a literal or a parenthesized expression after
// DEFAULT; Revision 2, A5). postgres: an index class other than UNIQUE
// (CREATE <class> INDEX, postgres migrator.go:138-142; Revision 2, A5).
func (b *builder) dialectFailure(sc *gschema, idx []*gindex) string {
	if b.has(DialectPostgres) {
		for _, ix := range idx {
			if ix.Class != "" && !strings.EqualFold(ix.Class, "UNIQUE") {
				return "postgres: the index " + ix.Name + " has class " + ix.Class + " (CREATE " + ix.Class + " INDEX is not postgres syntax)"
			}
		}
	}
	if !b.has(DialectSQLite) {
		return ""
	}
	n := 0
	for _, name := range sc.DBNames {
		f := sc.ByDB[name]
		if !f.IgnoreMig && f.AI && (f.DataType == "int" || f.DataType == "uint") {
			n++
		}
	}
	if n > 1 {
		return "two autoincrement integer fields: sqlite writes both as the primary key"
	}
	for _, ix := range idx {
		if ix.Type != "" || ix.Class != "" && ix.Class != "UNIQUE" {
			return "the index " + ix.Name + " has a type or class sqlite does not accept"
		}
	}
	for _, name := range sc.DBNames {
		f := sc.ByDB[name]
		if !f.IgnoreMig && f.HasDefault && !f.Parsed && reBareCall.MatchString(f.Default) && f.RawWhy == "" {
			return "sqlite: the default " + strconv.Quote(f.Default) + " of " + f.Name + " is a function call not wrapped in parentheses, which sqlite does not accept after DEFAULT"
		}
	}
	return ""
}

// reBareCall is a raw default that starts as a function call, name(...):
// sqlite needs DEFAULT (name(...)).
var reBareCall = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\s*\(`)

// gindex is one index the struct's tags declare (schema/index.go).
type gindex struct {
	// Name is the name on the group's first dialect; names the name per
	// dialect (a generated name is hashed at each dialect's limit).
	Name               string
	names              map[string]string
	Class, Type, Where string
	opts               []gindexOpt
	// columnsWhy is set when the columns are not known.
	columnsWhy string
	file       string
	line       int
}

type gindexOpt struct {
	field *gfield
	expr  bool
	desc  bool
	prio  int
}

// indexes ports ParseIndexes and parseFieldIndexes (index.go:30-167) for
// a struct; the reason is set when the struct's index list is not known.
func (b *builder) indexes(sc *gschema) ([]*gindex, string) {
	var out []*gindex
	byName := map[string]*gindex{}
	why := ""
	for _, f := range sc.Fields {
		if f.EmbeddedTagged && why == "" {
			why = "the embedding field of " + f.Name + " has tags, which GORM copies into the embedded fields' settings"
		}
		if f.Tags["INDEX"] == "" && f.Tags["UNIQUEINDEX"] == "" {
			continue
		}
		for _, value := range strings.Split(f.Raw, ";") {
			if value == "" {
				continue
			}
			v := strings.Split(value, ":")
			k := strings.TrimSpace(strings.ToUpper(v[0]))
			if k != "INDEX" && k != "UNIQUEINDEX" {
				continue
			}
			b.tag("GO-I1")
			tag := strings.Join(v[1:], ":")
			comma := strings.IndexByte(tag, ',')
			settings := parseTagSetting(strings.Join(strings.Split(tag, ",")[1:], ","), ",")
			if comma == -1 {
				comma = len(tag)
			}
			name := tag[:comma]
			names := map[string]string{}
			if name == "" {
				sub := f.Name
				if c, ok := settings["COMPOSITE"]; ok {
					if c == "" || c == "COMPOSITE" {
						return nil, "the composite setting of " + sc.Name + "." + f.Name + " is empty (index.go:118-124)"
					}
					sub = c
				}
				if !asciiName(sub) {
					return nil, "the generated index name of " + sc.Name + "." + f.Name + " is not computed (non-ASCII)"
				}
				for _, d := range b.m.Dialects {
					names[d] = b.namer(d).IndexName(sc.Table, sub)
				}
				name = names[b.m.Dialects[0]]
			} else {
				for _, d := range b.m.Dialects {
					names[d] = name
				}
			}
			class := settings["CLASS"]
			if k == "UNIQUEINDEX" || settings["UNIQUE"] != "" {
				class = "UNIQUE"
			}
			prio := 10
			if n, err := strconv.Atoi(settings["PRIORITY"]); err == nil {
				prio = n
			}
			ix := byName[name]
			if ix == nil {
				ix = &gindex{Name: name, names: names, file: f.File, line: f.Line}
				byName[name] = ix
				out = append(out, ix)
			}
			if ix.Class == "" {
				ix.Class = class
			}
			if ix.Type == "" {
				ix.Type = settings["TYPE"]
			}
			if ix.Where == "" {
				ix.Where = settings["WHERE"]
			}
			ix.opts = append(ix.opts, gindexOpt{field: f, expr: settings["EXPRESSION"] != "", desc: strings.EqualFold(settings["SORT"], "desc"), prio: prio})
		}
	}
	for _, ix := range out {
		// GORM sorts with sort.Slice, which is an insertion sort (stable)
		// for up to 12 elements; beyond that equal priorities may move.
		sort.SliceStable(ix.opts, func(i, j int) bool { return ix.opts[i].prio < ix.opts[j].prio })
		if len(ix.opts) > 12 {
			ix.columnsWhy = "the index has more than 12 fields: GORM's unstable sort may reorder equal priorities"
		}
	}
	return out, why
}

// table builds one table: a model's, or a join table's (GO-R2).
func (b *builder) table(mt *mtable, ref tableRef, sc *gschema, twoSided bool) *models.Table {
	m := b.m
	if sc.Join {
		b.tag("GO-R2")
		if why := b.joinAgree(mt); why != "" {
			b.notClaimed("D", ref.file, ref.line, "join table %s is not claimed: %s", sc.Table, why)
			return nil
		}
	} else {
		b.tag("GO-M1")
	}
	b.tag("GO-K4")
	idx, idxWhy := b.idx[sc], b.idxWhy[sc]
	if mt.indexWhy != "" && idxWhy == "" {
		idxWhy = mt.indexWhy
	}
	if len(mt.skipIndex) > 0 {
		var kept []*gindex
		for _, ix := range idx {
			skip := false
			for _, n := range uniqueNames(ix) {
				skip = skip || mt.skipIndex[n]
			}
			if !skip {
				kept = append(kept, ix)
			}
		}
		idx = kept
	}
	t := &models.Table{
		Identity: ref.identity, File: ref.file, Name: sc.Table, Model: ref.model, ModelLine: ref.line,
		Span: contract.Span{StartLine: ref.line, EndLine: ref.endLine}, SchemaKnown: true,
		ColumnsComplete: true, ConstraintsComplete: true,
	}
	st := sc.Ref.Struct
	if ref.structName != "" {
		for _, n := range b.in.Files[ref.file].Nodes {
			if n.Kind == "struct" && n.Name == st.Name && n.Span != nil && n.Span.StartLine <= st.Line && st.Line <= n.Span.EndLine {
				t.ModelIdentity = n.Identity
			}
		}
	}
	var notes []string
	if m.VersionStatus == VersionAssumed {
		notes = append(notes, VersionAssumption)
	}
	notes = append(notes, m.DriverNotes...)
	if len(notes) > 0 {
		t.Attributes = map[string]any{"assumption_notes": map[string]any{"GO-V1": strings.Join(notes, "; ")}}
	}
	reason := func(row string, fact *string, file string, line int, format string, args ...any) {
		t.Reasons = append(t.Reasons, models.Reason{Row: row, Fact: fact, File: file, Line: line, Reason: fmt.Sprintf(format, args...)})
	}
	mo := struct{ File string }{ref.file}
	for _, pr := range sc.Partial {
		t.ColumnsComplete = false
		reason(pr.Row, models.Fact("column_set"), pr.File, pr.Line, "%s", pr.Why)
	}
	// Primary key per dialect: sqlite's inline autoincrement key replaces
	// the table's (GO-C1).
	pkOpen := ""
	pkSets := map[string]string{}
	rs := map[*gfield]map[string]rendered{}
	typeWhy := map[*gfield]string{}
	for _, name := range sc.DBNames {
		f := sc.ByDB[name]
		rs[f] = map[string]rendered{}
		eff, why := b.fieldType(mt, sc, f)
		if why != "" {
			typeWhy[f] = why
		}
		for _, d := range m.Dialects {
			rs[f][d] = render(eff, d, m.MySQL)
			if !f.IgnoreMig && d == DialectSQLite && rs[f][d].TypeWhy != "" && strings.Contains(rs[f][d].TypeWhy, "PRIMARY KEY") {
				pkOpen = "the raw type of " + f.Name + " carries a PRIMARY KEY"
			}
		}
	}
	for _, d := range m.Dialects {
		var keys []string
		for _, name := range sc.DBNames {
			f := sc.ByDB[name]
			if !f.IgnoreMig && rs[f][d].InlinePK {
				keys = append(keys, name)
			}
		}
		if len(keys) == 0 {
			for _, f := range sc.Primary {
				keys = append(keys, f.DBName)
			}
		}
		pkSets[d] = strings.Join(keys, "\x00")
	}
	for _, f := range sc.Primary {
		if f.DBName == "" || f.IgnoreMig || sc.ByDB[f.DBName] != f {
			pkOpen = "the primary-key field " + f.Name + " has no column of its own"
		}
	}
	// A raw type text with more than a type name may hold constraints
	// (Revision 2, A1): the constraint set is partial, and with PRIMARY KEY
	// in it the key is not known.
	for _, name := range sc.DBNames {
		f := sc.ByDB[name]
		if f.IgnoreMig || f.RawWhy == "" {
			continue
		}
		b.tag("GO-T2")
		t.ConstraintsComplete = false
		reason("GO-T2", models.Fact("constraint_set"), f.File, f.Line, "%s", f.RawWhy)
		if strings.Contains(strings.ToUpper(f.DataType), "PRIMARY KEY") {
			pkOpen = "the raw type of " + f.Name + " carries a PRIMARY KEY"
		}
	}
	for _, d := range m.Dialects[1:] {
		if pkSets[d] != pkSets[m.Dialects[0]] {
			pkOpen = "the primary key differs per dialect (sqlite writes an autoincrement integer as the only primary key); per-dialect keys are not modelled"
		}
	}
	if pkOpen != "" {
		t.PrimaryKeyOpen = true
		reason("GO-C1", models.Fact("constraint_set"), mo.File, st.Line, "%s", pkOpen)
	} else if twoSided {
		// Open point O8: the side whose join schema GORM
		// parses last decides the key order.
		t.PrimaryKeyOpen = true
		reason("GO-R2", models.Fact("constraint_set"), mo.File, st.Line, "the join table %s is declared by several many2many relations: the primary-key column order depends on which side GORM parses last (open point O8)", sc.Table)
	}
	pk := map[string]bool{}
	for _, k := range strings.Split(pkSets[m.Dialects[0]], "\x00") {
		pk[k] = true
	}
	// Foreign keys (GO-R3).
	fks := b.foreignKeys(mt, sc)
	if fks.allWhy != "" {
		t.ConstraintsComplete = false
		reason(fks.allRow, models.Fact("constraint_set"), mo.File, st.Line, "%s", fks.allWhy)
	}
	for _, why := range fks.unknown {
		t.ConstraintsComplete = false
		reason(why.row, models.Fact("constraint_set"), why.file, why.line, "%s", why.why)
	}
	// Migrator DDL calls (Revision 2, A3) and DDL text (Revision 2
	// addendum).
	changedWhy, changedCols := b.changes(t, sc)
	if rawWhy := b.rawChanges(t, sc, changedCols); changedWhy == "" {
		changedWhy = rawWhy
	}
	// Indexes, unique constraints and checks.
	elementsWhy := idxWhy
	if elementsWhy == "" {
		elementsWhy = changedWhy
	}
	for _, f := range sc.Fields {
		if _, ok := f.Tags["CHECK"]; ok && !f.IgnoreMig {
			t.ConstraintsComplete = false
			b.tag("D")
			reason("D", models.Fact("constraint_set"), f.File, f.Line, "the check tag of %s is not read", f.Name)
		}
	}
	for _, ix := range idx {
		if ix.Class != "" && ix.Class != "UNIQUE" && (b.has(DialectPostgres) || b.has(DialectSQLite)) && elementsWhy == "" {
			elementsWhy = "the index " + ix.Name + " has class " + ix.Class + ", which is not read on postgres"
		}
	}
	columnSet := map[string]bool{}
	for _, name := range sc.DBNames {
		if !sc.ByDB[name].IgnoreMig {
			columnSet[name] = true
		}
	}
	indexed := map[string]bool{}
	uniqueIdx := map[string]bool{}
	if elementsWhy != "" {
		t.ElementsUnknown = true
		t.ConstraintsComplete = false
		reason("GO-I1", models.Fact("constraint_set"), mo.File, st.Line, "%s", elementsWhy)
	} else {
		t.Indexes = []map[string]any{}
		for _, ix := range idx {
			t.Indexes = append(t.Indexes, b.indexEntry(ix, columnSet))
			if ix.columnsWhy != "" {
				continue
			}
			for _, o := range ix.opts {
				if !o.expr {
					indexed[o.field.DBName] = true
				}
			}
			if ix.Class == "UNIQUE" && len(ix.opts) == 1 && !ix.opts[0].expr && ix.Where == "" {
				uniqueIdx[ix.opts[0].field.DBName] = true
			}
		}
		t.Uniques = []map[string]any{}
		for _, f := range sc.Fields {
			if !f.Uniq || f.IgnoreMig || f.DBName == "" {
				continue
			}
			b.tag("GO-C4")
			t.Uniques = append(t.Uniques, map[string]any{"columns": []string{f.DBName}, "name": b.uniqueName(sc, f)})
		}
		t.Checks = []map[string]any{}
	}
	for _, name := range sc.DBNames {
		f := sc.ByDB[name]
		if f.IgnoreMig {
			continue
		}
		c := b.column(t, sc, f, rs[f], pk[name], fks, indexed, uniqueIdx, elementsWhy == "", typeWhy[f])
		why, changed := changedCols["*"]
		if !changed {
			why, changed = changedCols[f.DBName]
		}
		if !changed {
			why, changed = changedCols[f.Name]
		}
		if c != nil && changed {
			// The column may be dropped, renamed or altered: no fact of it
			// is claimed (Revision 2, A3).
			b.tag("GO-K3")
			c = &models.Column{Name: c.Name, Attribute: c.Attribute, File: c.File, Line: c.Line, Unknown: true, Attributes: map[string]any{}, Claims: map[string]models.ClaimSource{},
				Reasons: []models.Reason{{Row: "GO-K3", File: f.File, Line: f.Line, Reason: why}}}
		}
		if c != nil {
			t.Columns = append(t.Columns, c)
		}
	}
	if !t.ColumnsComplete {
		t.ConstraintsComplete = false
	}
	return t
}

// namer is the naming strategy dialect d applies to the module's literal
// naming: a postgres driver older than v1.5.6 does not set the 63-byte
// limit, so GORM hashes at 64 and postgres truncates the 64-byte name
// (Revision 2, A7: such a name is then not claimed).
func (b *builder) namer(d string) namer {
	n := newNamer(b.m.Config.Naming.Naming, d)
	if d == DialectPostgres && b.m.OldPostgresNaming && b.m.Config.Naming.Naming.IdentifierMaxLength <= 0 {
		n.limit = 64
	}
	return n
}

// uniqueName is the unique constraint's name: GORM's UniqueName on
// postgres and mysql; nil on sqlite (the catalog keeps an autoindex name)
// and wherever the dialects' names differ.
func (b *builder) uniqueName(sc *gschema, f *gfield) any {
	if b.has(DialectSQLite) || !asciiName(f.DBName) {
		return nil
	}
	var name string
	for i, d := range b.m.Dialects {
		n := b.namer(d).UniqueName(sc.Table, f.DBName)
		if i > 0 && n != name || d == DialectPostgres && len(n) > 63 || d == DialectMySQL && len(n) > 64 {
			return nil
		}
		name = n
	}
	return name
}

// indexEntry is a table's index entry: name (nil when the dialects'
// names differ or postgres truncates it), columns in order (nil with an
// expression), unique, partial (the predicate text is not claimed) and
// the descending flags.
func (b *builder) indexEntry(ix *gindex, columnSet map[string]bool) map[string]any {
	var name any = ix.Name
	for _, d := range b.m.Dialects {
		n := ix.names[d]
		if n != ix.Name || d == DialectPostgres && len(n) > 63 || d == DialectMySQL && len(n) > 64 {
			name = nil
		}
	}
	var cols, desc any
	if ix.columnsWhy == "" {
		var cs []string
		var ds []bool
		for _, o := range ix.opts {
			if o.expr || !columnSet[o.field.DBName] {
				cs, ds = nil, nil
				break
			}
			cs = append(cs, o.field.DBName)
			ds = append(ds, o.desc)
		}
		if cs != nil {
			cols, desc = cs, ds
		}
	}
	return map[string]any{"name": name, "columns": cols, "unique": ix.Class == "UNIQUE", "partial": ix.Where != "", "where": nil, "descending": desc}
}

// toolType is the GORM data type with its size, precision and scale, as
// the tool's own type annotation (not a database fact).
func toolType(f *gfield) string {
	s := f.DataType
	if f.Size != 0 {
		s += fmt.Sprintf(", size %d", f.Size)
	}
	if f.Precision != 0 {
		s += fmt.Sprintf(", precision %d", f.Precision)
	}
	if f.Scale != 0 {
		s += fmt.Sprintf(", scale %d", f.Scale)
	}
	return s
}

// column builds one column of a table.
func (b *builder) column(t *models.Table, sc *gschema, f *gfield, rs map[string]rendered, pk bool, fks fkInfo, indexed, uniqueIdx map[string]bool, idxKnown bool, typeWhy string) *models.Column {
	m := b.m
	name := f.DBName
	derived := !f.ColumnTag
	switch {
	case derived && !asciiName(f.Name) || !asciiName(name) && !f.ColumnTag:
		t.ColumnsComplete = false
		t.Reasons = append(t.Reasons, models.Reason{Row: "GO-F2", Fact: models.Fact("column_set"), File: f.File, Line: f.Line, Reason: "the column name of " + f.Name + " is not computed (non-ASCII)"})
		return nil
	case b.p.limitName > 0 && len(name) > b.p.limitName:
		t.ColumnsComplete = false
		t.Reasons = append(t.Reasons, models.Reason{Row: "GO-F2", Fact: models.Fact("column_set"), File: f.File, Line: f.Line, Reason: "the column name " + name + " is longer than 63 bytes (postgres truncates it)"})
		return nil
	}
	c := &models.Column{Name: name, Attribute: strings.Join(f.BindNames, "."), File: f.File, Line: f.Line, Attributes: map[string]any{}, Claims: map[string]models.ClaimSource{}}
	reason := func(row, fact string, format string, args ...any) {
		var fp *string
		if fact != "" {
			fp = models.Fact(fact)
		}
		c.Reasons = append(c.Reasons, models.Reason{Row: row, Fact: fp, File: f.File, Line: f.Line, Reason: fmt.Sprintf(format, args...)})
	}
	unknown := func(row, format string, args ...any) *models.Column {
		b.tag(row)
		c.Unknown = true
		reason(row, "", format, args...)
		return c
	}
	_, generated := f.Tags["GENERATED"]
	switch {
	case f.Unknown != "":
		t.ColumnsComplete = false
		return unknown(f.UnknownRow, "%s", f.Unknown)
	case sc.Dup[name]:
		return unknown("GO-F1", "several fields write the column %s (schema.go:236-258)", name)
	case f.EmbeddedTagged:
		return unknown("GO-F1", "the embedding field's tags are copied into this field's settings (field.go:437-439)")
	case generated:
		return unknown("GO-T2", "the generated tag (postgres driver 1.6+) is not read")
	}
	b.tag("GO-F1")
	group := verificationOf(m.Dialects[0])
	if len(m.Dialects) > 1 {
		group = models.VerificationOracle
	}
	claim := func(fact string, v any, row string) {
		b.tag(row)
		c.Attributes[fact] = v
		c.Claims[fact] = models.ClaimSource{Row: row, Verification: group}
	}
	runtime := len(m.Dialects) > 1
	dialectFact := func(d, fact string, v any, row string) {
		b.tag(row)
		src := models.ClaimSource{Row: row, Verification: verificationOf(d)}
		if runtime {
			c.SetDialectFact(d, fact, v, src)
			return
		}
		c.Attributes[fact] = v
		if fact != "server_default_text" {
			c.Claims[fact] = src
		}
	}
	// Name (GO-F2 for a column tag or an embedded prefix, else GO-K4).
	if f.ColumnTag || f.Prefixed {
		claim("name", name, "GO-F2")
	} else {
		claim("name", name, "GO-K4")
	}
	b.tag("GO-T1")
	c.SetToolType(toolType(f))
	// Type and modifiers per dialect (GO-T2, GO-T1).
	if why := typeWhy; why != "" {
		reason("GO-T1", "type", "the type is not claimed: %s", why)
	} else {
		for _, d := range m.Dialects {
			r := rs[d]
			if r.Type == "" {
				reason("GO-T2", "type", "%s: %s", d, r.TypeWhy)
				continue
			}
			dialectFact(d, "type", r.Type, "GO-T2")
			for _, mod := range []struct {
				name string
				v    *int
			}{{"length", r.Length}, {"precision", r.Precision}, {"scale", r.Scale}} {
				if mod.v != nil {
					dialectFact(d, mod.name, *mod.v, "GO-T1")
				}
			}
		}
	}
	// Autoincrement per dialect (GO-C1).
	for _, d := range m.Dialects {
		r := rs[d]
		if r.NotNullWhy != "" && (r.Type == "" || f.SerialRaw != "") {
			reason("GO-C1", "autoincrement", "%s: %s", d, r.NotNullWhy)
			continue
		}
		dialectFact(d, "autoincrement", r.AI, "GO-C1")
	}
	// Primary key (GO-C1).
	if !t.PrimaryKeyOpen {
		claim("primary_key", pk, "GO-C1")
	} else {
		reason("GO-C1", "primary_key", "the table's primary key is not known")
	}
	// Nullability (GO-C2): one value on every dialect.
	nn, nnWhy := rs[m.Dialects[0]].NotNull, ""
	for _, d := range m.Dialects {
		r := rs[d]
		if r.NotNullWhy != "" {
			nnWhy = d + ": " + r.NotNullWhy
		} else if r.NotNull != nn {
			nnWhy = "the column is NOT NULL on some dialects only (" + strings.Join(m.Dialects, ", ") + "); per-dialect nullability is not modelled"
		}
	}
	if nnWhy == "" {
		claim("nullable", !nn, "GO-C2")
	} else {
		reason("GO-C2", "nullable", "%s", nnWhy)
	}
	// Unique and index (GO-C4, GO-I1).
	switch {
	case f.RawWhy != "":
		reason("GO-C4", "unique", "%s", f.RawWhy)
		if idxKnown {
			claim("index", indexed[name], "GO-I1")
		} else {
			reason("GO-I1", "index", "the table's index list is not known")
		}
	case !idxKnown:
		reason("GO-I1", "unique", "the table's index list is not known")
		reason("GO-I1", "index", "the table's index list is not known")
	default:
		switch {
		case f.Uniq:
			claim("unique", true, "GO-C4")
		case uniqueIdx[name]:
			claim("unique", true, "GO-I1")
		default:
			claim("unique", false, "GO-C4")
		}
		claim("index", indexed[name], "GO-I1")
	}
	// Default (GO-C3).
	b.defaults(c, rs, claim, dialectFact, reason)
	// Foreign keys (GO-R3).
	switch {
	case fks.allWhy != "":
		c.ForeignKeysUnknown = true
		reason(fks.allRow, "foreign_keys", "%s", fks.allWhy)
	case f.RawWhy != "":
		// The raw text may hold a REFERENCES clause (Revision 2, A1).
		c.ForeignKeysUnknown = true
		reason("GO-T2", "foreign_keys", "%s", f.RawWhy)
	case fks.colWhy[f] != nil:
		c.ForeignKeysUnknown = true
		reason(fks.colWhy[f].row, "foreign_keys", "%s", fks.colWhy[f].why)
	default:
		c.ForeignKeys = fks.byCol[f]
		claim("foreign_keys", []any{}, "GO-R3")
	}
	return c
}

// defaults states the default facts: default ("server" or "absent") when
// every dialect agrees, server_default and its text per dialect.
func (b *builder) defaults(c *models.Column, rs map[string]rendered, claim func(string, any, string), dialectFact func(string, string, any, string), reason func(string, string, string, ...any)) {
	m := b.m
	agree, present := true, false
	for i, d := range m.Dialects {
		df := rs[d].Default
		if !df.presentKnown {
			agree = false
			reason("GO-C3", "server_default", "%s: %s", d, df.Why)
			continue
		}
		if i == 0 {
			present = df.Present
		} else if df.Present != present {
			agree = false
		}
		dialectFact(d, "server_default", df.Present, "GO-C3")
		if df.Present {
			if df.Text != nil {
				dialectFact(d, "server_default_text", *df.Text, "GO-C3")
			} else {
				reason("GO-C3", "server_default_text", "%s: %s", d, df.TextWhy)
			}
		}
	}
	switch {
	case !agree:
		reason("GO-C3", "default", "the default is not the same on every dialect (%s)", strings.Join(m.Dialects, ", "))
	case present:
		claim("default", "server", "GO-C3")
	default:
		claim("default", "absent", "GO-C3")
	}
}

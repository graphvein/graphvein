package gorm

import (
	"fmt"
	"sort"
	"strings"
)

// This file simulates the module's migrate calls (rows GO-K3, GO-R1-R3,
// GO-P1) the way GORM runs them: AutoMigrate reorders its values with
// ReorderModels (migrator.go:919-1020: belongs-to constraint targets and
// join tables auto-added, each value after its dependencies) and creates
// each table that does not exist yet (migrator.go:122-206), stopping at
// the first error; Migrator().CreateTable creates only its values
// (ReorderModels(values, false)). GORM's schema cache makes some facts
// depend on when a schema is first parsed: a has-relation registers on the
// child, and a referenced type is copied onto a foreign key, only when the
// relation's owner is parsed. Parsing a value parses every
// schema its relations reach (its closure), before any table of the call
// is created.
//
// With the order proven (one sequence holding every site, open point O6)
// the calls run in that order. Otherwise each call is simulated alone and
// a fact that another call could change is unknown (tri-state below).

// tri is a three-valued answer: yes, no or unknown.
type tri int

const (
	triNo tri = iota
	triYes
	triUnknown
)

func triOr(a, b tri) tri {
	switch {
	case a == triYes || b == triYes:
		return triYes
	case a == triUnknown || b == triUnknown:
		return triUnknown
	}
	return triNo
}

// mcall is one migrate call the simulation runs.
type mcall struct {
	site    *MigrateSite
	values  []*gschema
	autoAdd bool
	// closure is every schema parsing the values reaches.
	closure map[*gschema]bool
}

// mseg is one segment of a call's order: the value inserted last and the
// dependencies inserted before it (insertIntoOrderedList), in the order
// the port inserts them; the order among members that are not each
// other's dependencies follows GORM's map iteration and is not known.
type mseg struct {
	members []*gschema
	// deps are the dependencies of each member in this call.
	deps map[*gschema][]*gschema
}

// mtable is one table name across the simulation.
type mtable struct {
	name string
	// schemas are the schemas created under the name (two for a join
	// table declared from both sides, open point O8); first is the one
	// the records use.
	schemas []*gschema
	// touches are the calls whose order holds the table (AutoMigrate) or
	// that create it (CreateTable); create is the call that creates it.
	touches []int
	create  int
	seg     int
	created bool
	// failed says why the table's creation fails (GO-P1); withheld why it
	// is not created because an earlier model of its call failed.
	failed, withheld, withheldRow string
	// skipIndex lists index names postgres skips on this table (another
	// table created earlier has them); indexWhy says why its index list is
	// not known.
	skipIndex map[string]bool
	indexWhy  string
}

// world is one module's simulation.
type world struct {
	b       *builder
	calls   []*mcall
	ordered bool
	// parsedAt is, with the order proven, the first call whose closure
	// holds the schema; anyClosure is every schema some call parses.
	parsedAt   map[*gschema]int
	anyClosure map[*gschema]bool
	tables     map[string]*mtable
	order      []*mtable
	cons       map[*grel]*gcon
	// segOf places a created table in its call: call index and segment.
	segOf map[*gschema][2]int
	// segDeps is the transitive dependency set of each created schema in
	// its segment, cyc the schemas in a dependency cycle there.
	segDeps map[*gschema]map[*gschema]bool
	cyc     map[*gschema]bool
	// maybe are tables a call may touch only through a registration
	// another call's parse could make (unordered calls).
	maybe map[string]string
}

// newWorld builds the call list: the proven sequence when the order is
// proven, else every proven site.
func (b *builder) newWorld(byModel map[string]*gschema) *world {
	m := b.m
	w := &world{b: b, ordered: m.OrderProven, parsedAt: map[*gschema]int{}, anyClosure: map[*gschema]bool{}, tables: map[string]*mtable{}, cons: map[*grel]*gcon{}, segOf: map[*gschema][2]int{}, segDeps: map[*gschema]map[*gschema]bool{}, cyc: map[*gschema]bool{}, maybe: map[string]string{}}
	var sites []*MigrateSite
	if w.ordered {
		sites = m.Sequences[0]
	} else {
		sites = m.Sites
	}
	for _, s := range sites {
		if !s.Proven() {
			continue
		}
		c := &mcall{site: s, autoAdd: s.Call != "Migrator().CreateTable", closure: map[*gschema]bool{}}
		for _, a := range s.Args {
			if a.Model == nil {
				continue
			}
			if sc := byModel[a.Model.Identity()]; sc != nil {
				c.values = append(c.values, sc)
			}
		}
		if len(c.values) == 0 {
			continue
		}
		for _, v := range c.values {
			closureOf(v, c.closure)
		}
		for s := range c.closure {
			w.anyClosure[s] = true
			if _, ok := w.parsedAt[s]; !ok {
				w.parsedAt[s] = len(w.calls)
			}
		}
		w.calls = append(w.calls, c)
	}
	return w
}

// closureOf adds every schema parsing s reaches: relation targets and
// join tables, transitively.
func closureOf(s *gschema, out map[*gschema]bool) {
	if s == nil || out[s] {
		return
	}
	out[s] = true
	for _, r := range s.Rels {
		closureOf(r.FieldSchema, out)
		closureOf(r.Join, out)
	}
}

// visible says whether the owner of a registered relation is parsed by
// call k: with the order proven, by its first parse; else yes when call
// k's own closure holds it, no when no call parses it, unknown otherwise.
func (w *world) visible(owner *gschema, k int) tri {
	if w.ordered {
		if at, ok := w.parsedAt[owner]; ok && at <= k {
			return triYes
		}
		return triNo
	}
	switch {
	case w.calls[k].closure[owner]:
		return triYes
	case w.anyClosure[owner]:
		return triUnknown
	}
	return triNo
}

func (w *world) constraint(r *grel) *gcon {
	if c, ok := w.cons[r]; ok {
		return c
	}
	c := w.b.constraint(r)
	w.cons[r] = c
	return c
}

// rels lists a schema's relations GORM iterates at call k: its own and
// the registered ones whose owner is parsed (want: yes only, or yes and
// unknown).
func (w *world) rels(s *gschema, k int, possible bool) []*grel {
	out := append([]*grel(nil), s.Rels...)
	for _, r := range s.Registered {
		v := w.visible(r.Schema, k)
		if v == triYes || possible && v == triUnknown {
			out = append(out, r)
		}
	}
	return out
}

// tableKey keys a schema in ReorderModels' valuesMap: its table name, or
// a key of its own when the name is not known.
func tableKey(s *gschema) string {
	if s.Table == "" {
		return "\x00" + s.Ref.key()
	}
	return s.Table
}

// reorder ports ReorderModels for call k: the segments in order.
func (w *world) reorder(k int, possible bool) []mseg {
	c := w.calls[k]
	ignore := w.b.m.Config.IgnoreRelationships
	parsed := map[*gschema]bool{}
	type dep struct {
		s    *gschema
		deps []*gschema
	}
	valuesMap := map[string]*dep{}
	var modelNames []string
	var parseDep func(s *gschema, add bool)
	parseDep = func(s *gschema, add bool) {
		if parsed[s] {
			return
		}
		parsed[s] = true
		d := &dep{s: s}
		var deferred []func()
		if !(ignore.Known && ignore.Value) {
			beDependedOn := map[*gschema]bool{}
			rels := w.rels(s, k, possible)
			for _, r := range rels {
				if r.Field.IgnoreMig {
					continue
				}
				if con := w.constraint(r); con != nil && con.Schema == s && con.Ref != s {
					d.deps = append(d.deps, con.Ref)
				}
				if r.Kind == relHasOne || r.Kind == relHasMany {
					beDependedOn[r.FieldSchema] = true
				}
			}
			for _, r := range rels {
				if r.Field.IgnoreMig || r.Join == nil {
					continue
				}
				r := r
				deferred = append(deferred, func() {
					// The many2many target's dependency is appended after
					// valuesMap holds a copy: no effect.
					if beDependedOn[r.FieldSchema] {
						parseDep(r.FieldSchema, c.autoAdd)
					}
					parseDep(r.Join, c.autoAdd)
				})
			}
		}
		if old := valuesMap[tableKey(s)]; old != nil && old.s != s {
			// Another schema with the same table name: the later one
			// replaces it (a join table from both sides, or a clash).
			t := w.table(tableKey(s))
			w.addSchema(t, old.s)
			w.addSchema(t, s)
		}
		valuesMap[tableKey(s)] = d
		if add {
			modelNames = append(modelNames, tableKey(s))
		}
		for i := len(deferred) - 1; i >= 0; i-- {
			deferred[i]()
		}
	}
	for _, v := range c.values {
		parseDep(v, true)
	}
	inserted := map[string]bool{}
	var orderedNames []string
	var insert func(name string)
	insert = func(name string) {
		if inserted[name] {
			return
		}
		inserted[name] = true
		if c.autoAdd {
			for _, d := range valuesMap[name].deps {
				if _, ok := valuesMap[tableKey(d)]; !ok {
					parseDep(d, c.autoAdd)
				}
				insert(tableKey(d))
			}
		}
		orderedNames = append(orderedNames, name)
	}
	var segs []mseg
	names := append([]string(nil), modelNames...)
	for _, n := range names {
		start := len(orderedNames)
		insert(n)
		if len(orderedNames) == start {
			continue
		}
		seg := mseg{deps: map[*gschema][]*gschema{}}
		for _, x := range orderedNames[start:] {
			d := valuesMap[x]
			seg.members = append(seg.members, d.s)
			seg.deps[d.s] = d.deps
		}
		segs = append(segs, seg)
	}
	return segs
}

// failure is one table creation of a call that fails or may fail.
type failure struct {
	x   *gschema
	why string
	// certain marks a failure at a known position (the table's
	// dependencies are created before it); row is GO-P1 for a provable
	// failure, D for one that depends on what is not read.
	certain bool
	row     string
}

// run simulates every call.
func (w *world) run() {
	m := w.b.m
	cfg := m.Config
	fkOff := cfg.DisableForeignKeys.Known && cfg.DisableForeignKeys.Value || cfg.IgnoreRelationships.Known && cfg.IgnoreRelationships.Value
	fkOn := cfg.DisableForeignKeys.Known && !cfg.DisableForeignKeys.Value && cfg.IgnoreRelationships.Known && !cfg.IgnoreRelationships.Value
	checksRefs := !fkOff && (w.b.has(DialectPostgres) || w.b.has(DialectMySQL))
	exists := map[string]bool{}
	// indexes holds the index names created so far and their table.
	indexes := map[string]*mtable{}
	for k, c := range w.calls {
		if !w.ordered {
			exists = map[string]bool{}
			indexes = map[string]*mtable{}
		}
		segs := w.reorder(k, false)
		certain := map[string]bool{}
		for _, s := range segs {
			for _, x := range s.members {
				certain[tableKey(x)] = true
			}
		}
		for _, s := range w.reorder(k, true) {
			for _, x := range s.members {
				if !certain[tableKey(x)] {
					w.maybe[tableKey(x)] = fmt.Sprintf("%s:%d: %s may also create the table of %s: whether a relation registers on a model it lists depends on a migrate order that is not proven", c.site.File, c.site.Line, c.site.Call, schemaName(x))
				}
			}
		}
		stopped := false
		for si, seg := range segs {
			if stopped {
				for _, x := range seg.members {
					w.withhold(x, "GO-P1", fmt.Sprintf("%s:%d: %s returns at an earlier model's error (migrator.go:122-206)", c.site.File, c.site.Line, c.site.Call))
				}
				continue
			}
			// Transitive dependencies and cycles inside the segment.
			in := map[*gschema]bool{}
			for _, x := range seg.members {
				in[x] = true
			}
			closure := map[*gschema]map[*gschema]bool{}
			for _, x := range seg.members {
				seen := map[*gschema]bool{}
				var walk func(s *gschema)
				walk = func(s *gschema) {
					for _, d := range seg.deps[s] {
						if in[d] && !seen[d] {
							seen[d] = true
							walk(d)
						}
					}
				}
				walk(x)
				closure[x] = seen
				if seen[x] {
					w.cyc[x] = true
				}
			}
			var fails []failure
			created := map[*gschema]bool{}
			for _, x := range seg.members {
				t := w.table(tableKey(x))
				w.addSchema(t, x)
				if exists[tableKey(x)] {
					continue
				}
				created[x] = true
				w.segOf[x] = [2]int{k, si}
				w.segDeps[x] = closure[x]
				if why := w.schemaFailure(x); why != "" {
					fails = append(fails, failure{x: x, why: why, certain: !w.cyc[x], row: "GO-P1"})
					continue
				}
				if !checksRefs {
					continue
				}
				for _, r := range w.rels(x, k, false) {
					con := w.constraint(r)
					if con == nil || con.Schema != x || con.Ref == x || r.Field.IgnoreMig {
						continue
					}
					ref := tableKey(con.Ref)
					var f failure
					switch {
					case exists[ref]:
						continue
					case closure[x][con.Ref] && !w.cyc[x]:
						continue
					case in[con.Ref]:
						f = failure{x: x, why: "a foreign-key cycle with " + schemaName(con.Ref) + ": one table of the cycle is created before the table it references", row: "GO-P1"}
					case !c.autoAdd && w.ordered:
						f = failure{x: x, why: "Migrator().CreateTable creates it with a foreign key to " + con.Ref.Table + ", which does not exist yet", certain: true, row: "GO-P1"}
					default:
						f = failure{x: x, why: "the table " + con.Ref.Table + " it references may not exist yet: the migrate order is not proven", row: "D"}
					}
					if !fkOn {
						f.why += " (if the configuration, which is not read, creates foreign keys)"
						f.certain, f.row = false, "D"
					}
					fails = append(fails, f)
				}
			}
			for _, x := range seg.members {
				if !created[x] {
					continue
				}
				if why := w.indexClash(x, k, si, indexes, closure); why != "" {
					fails = append(fails, failure{x: x, why: why, certain: !w.cyc[x] && !strings.Contains(why, "may be created first"), row: "GO-P1"})
				}
			}
			reached := map[*gschema]bool{}
			if len(fails) == 0 {
				for _, x := range seg.members {
					reached[x] = true
				}
			} else {
				// The call stops at the first failure. With one failing table
				// at a known position, its dependencies are created before it.
				stopped = true
				if len(fails) == 1 && fails[0].certain {
					reached = closure[fails[0].x]
				}
			}
			failedSet := map[*gschema]failure{}
			for _, f := range fails {
				if _, ok := failedSet[f.x]; !ok {
					failedSet[f.x] = f
				}
			}
			for _, x := range seg.members {
				t := w.tables[tableKey(x)]
				if reached[x] {
					t.touches = append(t.touches, k)
					exists[tableKey(x)] = true
					if created[x] && !t.created {
						t.created, t.create, t.seg = true, k, si
						w.order = append(w.order, t)
					}
					continue
				}
				if f, ok := failedSet[x]; ok {
					if t.failed == "" {
						t.failed, t.withheldRow = fmt.Sprintf("%s:%d: %s fails on %s: %s", c.site.File, c.site.Line, c.site.Call, schemaName(x), f.why), f.row
					}
					continue
				}
				if created[x] {
					w.withhold(x, "GO-P1", fmt.Sprintf("%s:%d: %s may return at another model's error before it creates the table of %s (migrator.go:122-206)", c.site.File, c.site.Line, c.site.Call, schemaName(x)))
				}
			}
		}
	}
}

// crossCalls handles, when the order is not proven, an index name two
// calls' tables share: which is created first is not known, so postgres'
// index lists of both are unknown and on sqlite either may fail its call
// (the table and the later models of its call are not claimed).
func (w *world) crossCalls() {
	if w.ordered {
		return
	}
	users := map[string][]*mtable{}
	for _, t := range w.order {
		seen := map[string]bool{}
		for _, ix := range w.b.idx[t.schemas[0]] {
			for _, n := range uniqueNames(ix) {
				if !seen[n] {
					seen[n] = true
					users[n] = append(users[n], t)
				}
			}
		}
	}
	var names []string
	for n := range users {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		list := users[n]
		calls := map[int]bool{}
		for _, t := range list {
			calls[t.create] = true
		}
		if len(calls) < 2 {
			continue
		}
		for _, t := range list {
			why := "the index name " + n + " is used by tables of calls whose order is not proven"
			if !w.b.has(DialectSQLite) {
				if t.indexWhy == "" {
					t.indexWhy = why + ": postgres skips the second CREATE INDEX IF NOT EXISTS"
				}
				continue
			}
			c := w.calls[t.create]
			if t.failed == "" {
				t.failed, t.withheldRow = fmt.Sprintf("%s:%d: %s may fail on %s: %s, and sqlite fails the second CREATE INDEX", c.site.File, c.site.Line, c.site.Call, t.name, why), "D"
			}
			for _, u := range w.order {
				if u.create == t.create && u.seg > t.seg && u.failed == "" {
					u.failed, u.withheldRow = fmt.Sprintf("%s:%d: %s may return at the error of %s before it creates %s (migrator.go:122-206)", c.site.File, c.site.Line, c.site.Call, t.name, u.name), "GO-P1"
				}
			}
		}
	}
}

func (w *world) table(key string) *mtable {
	t := w.tables[key]
	if t == nil {
		t = &mtable{name: key, skipIndex: map[string]bool{}}
		w.tables[key] = t
	}
	return t
}

func (w *world) addSchema(t *mtable, s *gschema) {
	for _, x := range t.schemas {
		if x == s {
			return
		}
	}
	t.schemas = append(t.schemas, s)
}

func (w *world) withhold(x *gschema, row, why string) {
	t := w.table(tableKey(x))
	w.addSchema(t, x)
	if !t.created && t.withheld == "" {
		t.withheld, t.withheldRow = why, row
	}
}

// schemaName names a schema in messages: the struct, or the join table's
// owner field.
func schemaName(s *gschema) string {
	if s.Join {
		return "the join table of " + s.Ref.Struct.Name
	}
	return s.Name
}

// schemaFailure is a provable failure of creating x (GO-P1): the struct's
// own (a field GORM cannot parse, a default that does not parse, a
// relation it cannot build, sqlite's two autoincrement fields or index
// type), or a schema its parse reaches that fails (GORM fails the parse
// of every schema whose relation targets it).
func (w *world) schemaFailure(x *gschema) string {
	if x.Failure != "" {
		return x.Failure
	}
	if why := w.b.dialectFailure(x, w.b.idx[x]); why != "" {
		return why
	}
	reach := map[*gschema]bool{}
	closureOf(x, reach)
	var names []string
	for s := range reach {
		if s.Failure != "" {
			names = append(names, s.Name)
		}
	}
	if len(names) > 0 {
		sort.Strings(names)
		return "its relations reach " + strings.Join(names, ", ") + ", which GORM cannot parse"
	}
	return ""
}

// indexClash checks x's index names against the indexes created before
// it: postgres skips a name another table already has (CREATE INDEX IF
// NOT EXISTS, migrator.go:149), sqlite fails the call (GO-P1). It returns
// sqlite's failure; where which table is created first is not known,
// postgres' index lists of both tables are unknown.
func (w *world) indexClash(x *gschema, k, si int, indexes map[string]*mtable, closure map[*gschema]map[*gschema]bool) string {
	t := w.tables[tableKey(x)]
	var fail string
	for _, ix := range w.b.idx[x] {
		for _, n := range uniqueNames(ix) {
			other := indexes[n]
			if other == nil || other == t {
				indexes[n] = t
				continue
			}
			before := triUnknown
			if pos, ok := w.segOfTable(other); ok {
				switch {
				case pos[0] < k && w.ordered, pos[0] == k && pos[1] < si:
					before = triYes
				case pos[0] == k && pos[1] == si:
					for _, s := range other.schemas {
						if closure[x][s] && !w.cyc[x] {
							before = triYes
						}
					}
				}
			}
			switch {
			case w.b.has(DialectSQLite):
				fail = "the index name " + n + " is used by the table " + other.name + " too: sqlite fails the second CREATE INDEX"
				if before != triYes {
					fail = "the index name " + n + " is used by the table " + other.name + ", which may be created first: sqlite fails the second CREATE INDEX"
					other.indexWhy = "the index name " + n + " is used by " + t.name + " too, and which table is created first is not known"
				}
			case before == triYes && !w.b.has(DialectMySQL):
				t.skipIndex[n] = true
			default:
				why := "the index name " + n + " is used by the tables " + other.name + " and " + t.name + ": postgres skips the second CREATE INDEX IF NOT EXISTS"
				if before != triYes {
					why += ", and which table is created first is not known"
				} else {
					why += " (mysql keeps index names per table)"
				}
				t.indexWhy = why
				if before != triYes {
					other.indexWhy = why
				}
			}
		}
	}
	return fail
}

// uniqueNames lists an index's distinct names over the dialects.
func uniqueNames(ix *gindex) []string {
	var out []string
	seen := map[string]bool{}
	for _, n := range ix.names {
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

func (w *world) segOfTable(t *mtable) ([2]int, bool) {
	for _, s := range t.schemas {
		if p, ok := w.segOf[s]; ok {
			return p, true
		}
	}
	return [2]int{}, false
}

// registered says whether a registered relation (owner rel.Schema) is on
// table t by the end: it is created at the table's creation or added by a
// later AutoMigrate of it once the owner is parsed.
func (w *world) registered(t *mtable, rel *grel) tri {
	out := triNo
	for _, k := range t.touches {
		out = triOr(out, w.visible(rel.Schema, k))
	}
	return out
}

// copied says whether a type copy made by owner's relation parse shows in
// table t's column: the copy exists at the table's creation and at every
// later AutoMigrate of it (GORM's MigrateColumn may alter the column
// otherwise), or at none of them.
func (w *world) copied(t *mtable, owner *gschema) tri {
	for _, s := range t.schemas {
		if s == owner {
			return triYes
		}
	}
	if len(t.touches) == 0 {
		return triUnknown
	}
	all, none := true, true
	for _, k := range t.touches {
		switch w.visible(owner, k) {
		case triYes:
			none = false
		case triNo:
			all = false
		default:
			all, none = false, false
		}
	}
	switch {
	case all:
		return triYes
	case none:
		return triNo
	}
	return triUnknown
}

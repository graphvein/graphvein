// Package sqlmodel links SQLModel model classes into schema facts at link
// time (T018c2, ADR 0054); it is the SQLModel front end of the model kit
// (internal/extract/models, T018g0, ADR 0058). It reads the model syntax that the Python extractor
// caches (T018c1, ADR 0053), resolves bases, callees and annotations through
// the Python module resolver (ADR 0015, ADR 0017) and emits model-defined
// table and column nodes with source "model".
//
// Every fact comes from one row of the claim contract
// (docs/extract/schema-models.md): what no row covers stays unknown, and a
// library default is claimed only where the class and its proven chain show
// the setting absent. Model tables are never merged into migration sets; a
// table name both sources define gets a diagnostic (row L5). The core
// package does L4, L5 and the node shapes (L3) for every ORM front end.
package sqlmodel

import (
	"fmt"
	"sort"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/python"
	"github.com/graphvein/graphvein/internal/testscope"
)

// Input carries the current Python facts, the shared resolver, the
// link-time sources (alias values are re-read from them), the test-scope
// rules and the migration pass's records for the comparison of row L5.
type Input struct {
	Python     map[string]facts.Facts
	Resolver   *python.ModuleResolver
	Sources    map[string][]byte
	Tests      testscope.Rules
	Migrations []contract.Node
	Edges      []contract.Edge
}

// Pins are the library versions the contract describes (V1); every model
// table states them with the import assumption V7.
var Pins = map[string]any{"sqlmodel": "0.0.39", "sqlalchemy": "2.0.51", "pydantic": "2.13.4"}

// claimTag is a test-only hook: every claim branch reports its contract row.
var claimTag func(row string)

// debugPanics lets a test see the stack of a class failure.
var debugPanics = false

// faultHook is a test-only hook called at the start of every isolated step
// with the step name and its subject (a class, site or table), so a test can
// make one step fail and check that the others still run.
var faultHook func(step, subject string)

func tag(row string) {
	if claimTag != nil {
		claimTag(row)
	}
}

type linker struct {
	in      Input
	classes map[classKey]*classRec
	tables  []*table
	steps   *models.Steps
	unread  map[string]int
}

type classRec struct {
	file string
	rec  *facts.PythonModelClass
}

// Link emits model-defined tables. A failure on one class never stops the
// others: a panic while evaluating a class becomes a diagnostic.
func Link(in Input) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	l := &linker{in: in, classes: map[classKey]*classRec{}, unread: map[string]int{}, steps: &models.Steps{Hook: faultHook, Panics: debugPanics}}
	files := make([]string, 0, len(in.Python))
	for f := range in.Python {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, f := range files {
		p := in.Python[f].Python
		if p == nil || p.Models == nil || !p.ModelsValid() {
			continue
		}
		for i := range p.Models.Classes {
			c := &p.Models.Classes[i]
			l.classes[classKey{f, c.Scope, c.Name}] = &classRec{file: f, rec: c}
		}
	}
	for _, f := range files {
		p := in.Python[f].Python
		if p == nil || p.Models == nil || !p.ModelsValid() {
			continue
		}
		for i := range p.Models.Classes {
			c := &p.Models.Classes[i]
			l.guard(f, "class", fmt.Sprintf("%s:%d: model class %s", f, c.Line, c.Name), func() { l.class(f, c) })
		}
		if n := len(p.Models.CoreTables); n > 0 {
			tag("U3")
			tag("U4")
			tag("L6")
			l.diag(f, "info", "schema_model_core_tables", fmt.Sprintf("%s: %d calls named Table or map_imperatively (Core tables or imperative mapping candidates), not read", f, n))
		}
	}
	for _, f := range files {
		p := in.Python[f].Python
		if p == nil || p.Models == nil || !p.ModelsValid() {
			continue
		}
		for _, s := range p.Models.Sites {
			l.guard(f, "site", fmt.Sprintf("%s:%d: %s site", f, s.Line, s.Kind), func() { l.site(f, s) })
		}
	}
	l.dropRefused()
	for _, f := range files {
		if n := l.unread[f]; n > 0 {
			tag("L6")
			l.diag(f, "info", "schema_model_unread", fmt.Sprintf("%s: %d model classes present, not read (see the per-class diagnostics)", f, n))
		}
	}
	records := make([]*models.Table, 0, len(l.tables))
	for _, t := range l.tables {
		l.guard(t.file, "record", "model table "+t.identity, func() { records = append(records, t.record()) })
	}
	nodes, edges := models.Link(records, models.LinkInput{Group: Group(), Rows: rows, Tag: claimTag, Steps: l.steps, Sources: in.Sources, Migrations: in.Migrations, Edges: in.Edges})
	return nodes, edges, l.steps.Diagnostics
}

func (l *linker) diag(file, severity, code, message string) {
	l.steps.Diag(file, severity, code, message)
}

// guard runs one per-file, per-class or per-site step so that a failure in
// it (a panic) becomes a diagnostic on file and never stops the other steps
// (the core's Steps.Guard; the core isolates the per-table steps).
func (l *linker) guard(file, step, subject string, fn func()) {
	l.steps.Guard(file, step, subject, fn)
}

// refuse records a class that claims no table, with the row that decided it.
func (l *linker) refuse(file string, c *facts.PythonModelClass, row, why string) {
	tag(row)
	l.unread[file]++
	l.diag(file, "info", "schema_model_not_claimed", fmt.Sprintf("%s:%d: model class %s: no table claimed (%s: %s)", file, c.Line, c.Name, row, why))
}

// dropRefused removes the tables and columns an X1 attribute assignment may
// rename (Rev 3.2). It runs after every site, so a later site on a refused
// table still resolves to it and affects nothing else. Each table is one
// isolated step (T018g0 Revision 2): a table whose step fails is not
// emitted, and the others are.
func (l *linker) dropRefused() {
	var kept []*table
	for _, t := range l.tables {
		l.guard(t.file, "refused", "model table "+t.identity, func() {
			if t.refusedWhy != "" {
				l.refuse(t.file, t.cls, "X1", t.refusedWhy)
				return
			}
			var cols []*column
			for _, c := range t.columns {
				if c.refused {
					delete(t.byName, c.name)
					continue
				}
				cols = append(cols, c)
			}
			t.columns = cols
			kept = append(kept, t)
		})
	}
	l.tables = kept
}

func keyword(c *facts.PythonModelClass, name string) (facts.PythonModelExpr, bool) {
	for _, k := range c.Keywords {
		if k.Name == name {
			return k.Value, true
		}
	}
	return facts.PythonModelExpr{}, false
}

func (l *linker) classOf(file string, c *facts.PythonModelClass) classKey {
	return classKey{file, c.Scope, c.Name}
}

// class decides rows C1-C6, U1 and the chain for one class record.
func (l *linker) class(file string, c *facts.PythonModelClass) {
	flag, hasTable := keyword(c, "table")
	if !hasTable {
		if l.declarative(file, c) {
			tag("U1")
			l.unread[file]++
			l.diag(file, "info", "schema_model_not_claimed", fmt.Sprintf("%s:%d: SQLAlchemy declarative class %s present, not read (U1)", file, c.Line, c.Name))
		}
		return
	}
	if flag.Kind != "literal" || flag.LitKind != "bool" {
		tag("C1c")
		l.unread[file]++
		l.diag(file, "info", "schema_model_flag_unknown", fmt.Sprintf("%s:%d: model class %s: table flag is not a literal; the class may be a table (C1c)", file, c.Line, c.Name))
		return
	}
	if flag.Literal != "True" {
		tag("C1b")
		return
	}
	if !c.MembersCaptured {
		l.refuse(file, c, "D", "members not captured")
		return
	}
	if c.Conditional {
		l.refuse(file, c, "D", "conditional definition")
		return
	}
	if len(c.Decorators) > 0 {
		l.refuse(file, c, "D", "decorated class")
		return
	}
	if b, ok := l.binding(file, c); !ok || b.Count != 1 || !b.Stable {
		l.refuse(file, c, "D", "the class name is bound more than once")
		return
	}
	for _, k := range c.Keywords {
		if k.Name != "table" {
			l.refuse(file, c, "C4", "class keyword "+k.Name)
			return
		}
	}
	if len(c.Bases) == 0 {
		l.refuse(file, c, "C6", "no base")
		return
	}
	for _, b := range c.Bases {
		switch l.baseKind(file, c.Scope, b, 0) {
		case baseTable:
			l.refuse(file, c, "C3", "a direct base is a table class")
			return
		case baseUnknown:
			tag("L2")
			l.unread[file]++
			l.diag(file, "info", "schema_model_not_proven", fmt.Sprintf("%s:%d: model class %s: a base is not proven non-table (R-B2); no table claimed", file, c.Line, c.Name))
			return
		}
	}
	tag("C1")
	t := l.buildTable(file, c)
	if t == nil {
		return
	}
	l.tables = append(l.tables, t)
}

func (l *linker) binding(file string, c *facts.PythonModelClass) (facts.PythonBinding, bool) {
	p := l.in.Python[file].Python
	if p == nil || c.Scope >= len(p.Scopes) {
		return facts.PythonBinding{}, false
	}
	b, ok := p.Scopes[c.Scope].Bindings[c.Name]
	return b, ok
}

// declarative reports a U1 header: a base resolving to SQLAlchemy's
// declarative bases, directly or (Rev 3) through repository classes, so
// `Account(Base)` with `Base(DeclarativeBase)` counts as well as Base.
func (l *linker) declarative(file string, c *facts.PythonModelClass) bool {
	return l.declarativeDepth(file, c, 0)
}

func (l *linker) declarativeDepth(file string, c *facts.PythonModelClass, depth int) bool {
	if depth > 20 {
		return false
	}
	for _, b := range c.Bases {
		switch l.canon(file, c.Scope, &b) {
		case "sqlalchemy.orm.DeclarativeBase", "sqlalchemy.orm.DeclarativeBaseNoMeta", "sqlalchemy.orm.MappedAsDataclass":
			return true
		}
		if b.Kind != "name" {
			continue
		}
		r := l.resolve(file, c.Scope, b.Name)
		if r.class == nil {
			continue
		}
		if cr := l.classes[*r.class]; cr != nil && l.declarativeDepth(cr.file, cr.rec, depth+1) {
			return true
		}
	}
	return false
}

type baseClass int

const (
	baseNonTable baseClass = iota
	baseTable
	baseUnknown
)

// baseKind applies R-B2 to one base expression: proven non-table, a table
// (C3), or unknown.
func (l *linker) baseKind(file string, scope int, e facts.PythonModelExpr, depth int) baseClass {
	if depth > 20 || e.Kind != "name" {
		return baseUnknown
	}
	r := l.resolve(file, scope, e.Name)
	switch r.canonical {
	case "sqlmodel.SQLModel", "builtins.object":
		return baseNonTable
	}
	if r.class == nil {
		return baseUnknown
	}
	cr := l.classes[*r.class]
	if cr == nil {
		// A repository class without a captured header has no base, keyword
		// or decorator (gate G2): a plain class, proven non-table.
		if len(r.binding.Syntax.Bases) == 0 {
			return baseNonTable
		}
		return baseUnknown
	}
	if flag, ok := keyword(cr.rec, "table"); ok {
		if flag.Kind != "literal" || flag.LitKind != "bool" {
			return baseUnknown
		}
		if flag.Literal == "True" {
			return baseTable
		}
	}
	for _, k := range cr.rec.Keywords {
		if k.Name != "table" {
			return baseUnknown
		}
	}
	if !cr.rec.MembersCaptured || !l.configReadable(cr.file, cr.rec) {
		return baseUnknown
	}
	result := baseNonTable
	for _, b := range cr.rec.Bases {
		switch l.baseKind(cr.file, cr.rec.Scope, b, depth+1) {
		case baseTable:
			return baseTable
		case baseUnknown:
			result = baseUnknown
		}
	}
	return result
}

// kCfg is the C5 allowlist of config keys that cannot change table-ness.
var kCfg = map[string]bool{"arbitrary_types_allowed": true, "validate_assignment": true, "from_attributes": true, "populate_by_name": true, "extra": true, "frozen": true, "use_enum_values": true, "str_strip_whitespace": true, "json_schema_extra": true, "validate_default": true, "ser_json_timedelta": true}

// configReadable applies C5 (M8, M9) to a class's model_config and Config.
func (l *linker) configReadable(file string, c *facts.PythonModelClass) bool {
	for _, m := range c.Members {
		switch {
		case m.Kind == "assign" && m.Name == "model_config":
			v := m.Value
			if v == nil {
				return false
			}
			if v.Kind == "call" && l.canon(file, c.Scope, v) == "pydantic.ConfigDict" {
				if len(v.Args) > 0 {
					return false
				}
				for _, k := range v.Keywords {
					if !kCfg[k.Name] || k.Value.Kind != "literal" {
						return false
					}
				}
				continue
			}
			if v.Kind == "dict" {
				for i, k := range v.Keys {
					if k.Kind != "literal" || k.LitKind != "str" || !kCfg[k.Literal] || i >= len(v.Args) || v.Args[i].Kind != "literal" {
						return false
					}
				}
				continue
			}
			return false
		case m.Kind == "class" && m.Name == "Config":
			for _, n := range m.Members {
				if n.Kind != "assign" || !kCfg[n.Name] || n.Value == nil || n.Value.Kind != "literal" {
					return false
				}
			}
		}
	}
	return true
}

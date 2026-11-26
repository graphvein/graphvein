package sqlmodel

import (
	"fmt"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/testscope"
)

// reason explains why a fact or a set is not known, by contract row.
// Fact names the attribute the reason makes unknown or withholds
// (T018f, ADR 0055): column_set or constraint_set for a table reason,
// type, primary_key or foreign_keys for a column reason, and null when
// the reason concerns the whole column claim (every attribute unknown).
// File is the file of Line, which for a chain member or a mutation site
// can differ from the table's file.
type reason = models.Reason

// fact returns a reason's fact name.
func fact(name string) *string { return models.Fact(name) }

// reasonFact is the fact a table reason withholds: the column set when the
// reason makes it incomplete (the constraint set then is too), else the
// constraint set.
func reasonFact(columns bool) *string {
	if columns {
		return fact("column_set")
	}
	return fact("constraint_set")
}

type table struct {
	file, identity, name string
	cls                  *facts.PythonModelClass
	test                 bool
	schemaKnown          bool
	columnsComplete      bool
	constraintsComplete  bool
	reasons              []reason
	columns              []*column
	byName               map[string]*column
	uniques, indexes     []map[string]any
	checks               []map[string]any
	ddlHooks             bool
	// pkOpen is true when the constraint set may hold a primary key, so the
	// table's primary_key list is unknown (Rev 3).
	pkOpen bool
	// elementsUnknown is true when a table attribute assignment (X1) may
	// replace the constraint elements read, so the lists are unknown.
	elementsUnknown bool
	// refusedWhy is set when an X1 attribute assignment may change the
	// table's name, schema or fullname (Rev 3.2): the class claims no table.
	refusedWhy string
}

func (t *table) partial(row, file string, line int, why string, columns, constraints bool) {
	tag(row)
	t.reasons = append(t.reasons, reason{Row: row, Fact: reasonFact(columns), File: file, Line: line, Reason: why})
	if columns {
		t.columnsComplete = false
	}
	if constraints {
		t.constraintsComplete = false
	}
}

func isDunder(name string) bool {
	return len(name) > 4 && strings.HasPrefix(name, "__") && strings.HasSuffix(name, "__")
}

var kDec = map[string]bool{"builtins.classmethod": true, "builtins.staticmethod": true, "builtins.property": true,
	"pydantic.field_validator": true, "pydantic.field_serializer": true, "pydantic.model_validator": true,
	"pydantic.model_serializer": true, "pydantic.validator": true, "pydantic.root_validator": true,
	"pydantic.computed_field": true, "sqlalchemy.orm.validates": true}

var forbiddenMethods = map[string]bool{"__tablename__": true, "__table_args__": true, "__table__": true, "__mapper_args__": true, "__init_subclass__": true}

var kMapper = map[string]bool{"confirm_deleted_rows": true, "eager_defaults": true}

// buildTable reads the chain (C2) and members of a claimed table class.
func (l *linker) buildTable(file string, c *facts.PythonModelClass) *table {
	t := &table{file: file, cls: c, columnsComplete: true, constraintsComplete: true, byName: map[string]*column{}}
	t.test = testscope.Classify(file, l.in.Tests) == testscope.Test
	t.identity = "table:" + file + "#" + c.Name
	chain := []*classRec{{file: file, rec: c}}
	multi := len(c.Bases) > 1
	if multi {
		t.partial("C6", file, c.Line, "several bases: field order and merge are not modelled", true, true)
	}
	for cur := chain[0]; !multi; {
		if len(chain) > 20 {
			t.partial("D", file, c.Line, "chain deeper than 20", true, true)
			multi = true
			break
		}
		if len(cur.rec.Bases) != 1 {
			t.partial("C6", cur.file, cur.rec.Line, "a chain class with several bases", true, true)
			multi = true
			break
		}
		r := l.resolve(cur.file, cur.rec.Scope, cur.rec.Bases[0].Name)
		if r.canonical == "sqlmodel.SQLModel" {
			break
		}
		next := (*classRec)(nil)
		if r.class != nil {
			next = l.classes[*r.class]
		}
		if next == nil || !next.rec.MembersCaptured {
			t.partial("C2", cur.file, cur.rec.Line, "an ancestor without a captured record", true, true)
			multi = true
			break
		}
		tag("C2")
		chain = append(chain, next)
		cur = next
	}
	// Members, nearest class first; the nearest definition wins wholesale.
	seen := map[string]bool{}
	unreadBefore := false // an unread member in a class nearer than or equal to the current one
	var tablename, tableArgs *facts.PythonModelMember
	var tableArgsFile string
	tablenameUnknown, argsUnknown := false, false
	var failure string
	for _, cr := range chain {
		scope := cr.rec.Scope
		unreadHere := false
		for i := range cr.rec.Members {
			m := cr.rec.Members[i]
			switch m.Kind {
			case "annotated":
				switch {
				case m.Name == "__tablename__":
					if tablename == nil && !tablenameUnknown {
						tag("N1")
						tablenameUnknown = true
					}
				case isDunder(m.Name):
					t.partial("M12", cr.file, m.Line, "annotated dunder "+m.Name, true, true)
					unreadHere = true
					if m.Name == "__table_args__" && tableArgs == nil {
						// M11: an annotated __table_args__ is not read.
						argsUnknown = true
					}
				case strings.HasPrefix(m.Name, "_"):
					tag("M7")
				case seen[m.Name]:
				default:
					seen[m.Name] = true
					col, skip, f := l.fieldColumn(cr.file, scope, m)
					if f != "" && failure == "" {
						failure = f
					}
					if skip || col == nil {
						continue
					}
					l.addColumn(t, col)
				}
			case "assign":
				switch {
				case m.Name == "__tablename__":
					if tablename == nil && !tablenameUnknown {
						if m.Value != nil && m.Value.Kind == "literal" && m.Value.LitKind == "str" && !unreadBefore && !unreadHere {
							tag("N1")
							tablename = &cr.rec.Members[i]
						} else {
							tag("N3")
							tablenameUnknown = true
						}
					}
				case m.Name == "__table_args__":
					if tableArgs == nil && !argsUnknown {
						tag("M11")
						if unreadBefore || unreadHere {
							argsUnknown = true
						} else {
							tableArgs, tableArgsFile = &cr.rec.Members[i], cr.file
						}
					}
				case m.Name == "model_config":
					tag("M8")
					tag("C5")
					if !l.configReadable(cr.file, cr.rec) {
						t.partial("C5", cr.file, m.Line, "model_config outside the allowlist", true, true)
						unreadHere = true
					}
				case m.Name == "__mapper_args__":
					tag("M10")
					ok := m.Value != nil && m.Value.Kind == "dict"
					if ok {
						for j, k := range m.Value.Keys {
							if k.Kind != "literal" || !kMapper[k.Literal] || j >= len(m.Value.Args) || m.Value.Args[j].Kind != "literal" {
								ok = false
							}
						}
					}
					if !ok {
						t.partial("M10", cr.file, m.Line, "__mapper_args__ outside the allowlist", true, true)
						unreadHere = true
					}
				case m.Name == "__table__":
					t.partial("U2", cr.file, m.Line, "__table__ assignment", true, true)
					unreadHere = true
				case strings.HasPrefix(m.Name, "_"):
					tag("M7")
				default:
					if failure == "" && !multi && m.Value != nil && m.Value.Kind == "literal" && l.literalRejected(chain, cr, m.Name) {
						failure = "M12: non-annotated attribute " + m.Name + " (pydantic raises PydanticUserError)"
					}
					t.partial("M12", cr.file, m.Line, "unannotated assignment "+m.Name, true, true)
					unreadHere = true
					if !seen[m.Name] {
						seen[m.Name] = true
						col := newColumn(m.Name, cr.file, m.Line)
						col.makeUnknown("M12", "unannotated assignment")
						t.unknownIfField(l, chain, col)
					}
				}
			case "method":
				tag("M1")
				bad := forbiddenMethods[m.Name] || seen[m.Name]
				for j := range m.Decorators {
					d := l.canon(cr.file, scope, &m.Decorators[j])
					if d == "sqlalchemy.orm.declared_attr" {
						tag("U2")
					}
					if !kDec[d] {
						bad = true
					}
				}
				if m.Name == "__tablename__" && tablename == nil {
					tablenameUnknown = true
				}
				if m.Name == "__table_args__" && tableArgs == nil {
					argsUnknown = true
				}
				if bad {
					t.partial("M1", cr.file, m.Line, "method "+m.Name+" the contract does not read", true, true)
					unreadHere = true
				}
			case "class":
				if m.Name == "Config" {
					tag("M9")
					tag("C5")
					if !l.configReadable(cr.file, cr.rec) {
						t.partial("C5", cr.file, m.Line, "class Config outside the allowlist", true, true)
						unreadHere = true
					}
				} else {
					t.partial("M12", cr.file, m.Line, "nested class "+m.Name, true, true)
					unreadHere = true
				}
			default:
				t.partial("M12", cr.file, m.Line, "statement "+m.Detail, true, true)
				unreadHere = true
				if tablename == nil {
					tablenameUnknown = true
				}
				if tableArgs == nil {
					argsUnknown = true
				}
			}
		}
		if unreadHere {
			unreadBefore = true
		}
	}
	// The constraint set holds no primary key only when it is proven: no
	// __table_args__ in the chain, or a tuple of TA1-TA3 calls only. Anything
	// else (a PrimaryKeyConstraint, an unread element or member, other bases)
	// may set the primary key, so neither the P1 failure below nor a column's
	// primary_key: false is claimed (Rev 3).
	pkRow := ""
	switch {
	case multi:
		pkRow = "C6"
	case argsUnknown:
		pkRow = "M11"
	case tableArgs != nil:
		pkRow = l.primaryKeyOpen(tableArgsFile, chainScope(chain, tableArgsFile), tableArgs)
	}
	if failure == "" && t.columnsComplete && pkRow == "" {
		pk := false
		for _, col := range t.columns {
			if col.attrs["primary_key"] != false {
				pk = true
			}
		}
		if !pk {
			failure = "PK: no primary-key column in a complete column set and no primary key in the constraint set (SQLAlchemy cannot assemble a primary key)"
		}
	}
	if failure != "" {
		tag("L7")
		tag("L7/" + failure[:strings.Index(failure, ":")])
		l.diag(file, "warning", "schema_model_import_failure", fmt.Sprintf("%s:%d: model class %s cannot be created (%s); no table claimed (P1)", file, c.Line, c.Name, failure))
		return nil
	}
	// Table name: N1, N2 or N3.
	switch {
	case tablename != nil:
		t.name = tablename.Value.Literal
	case tablenameUnknown || multi || unreadBefore:
		tag("N3")
		l.unread[file]++
		l.diag(file, "info", "schema_model_name_unknown", fmt.Sprintf("%s:%d: model class %s: table name unknown (N3); no table node", file, c.Line, c.Name))
		return nil
	default:
		for _, r := range c.Name {
			if r > 127 {
				tag("N3")
				l.unread[file]++
				l.diag(file, "info", "schema_model_name_unknown", fmt.Sprintf("%s:%d: model class %s: non-ASCII class name, table name unknown (N3)", file, c.Line, c.Name))
				return nil
			}
		}
		tag("N2")
		t.name = strings.ToLower(c.Name)
	}
	// Schema and constraints.
	switch {
	case argsUnknown:
		t.partial("M11", file, c.Line, "__table_args__ may be set by an unread member", false, true)
	case tableArgs != nil:
		l.tableArgs(t, tableArgsFile, chainScope(chain, tableArgsFile), tableArgs)
	case multi:
		t.partial("C6", file, c.Line, "schema and constraints of other bases are not read", false, true)
	default:
		t.schemaKnown = true
	}
	if pkRow != "" {
		t.openPrimaryKey(pkRow, file, c.Line, "the constraint set may hold a primary key")
	}
	return t
}

// openPrimaryKey records that the table's primary key may differ from its
// columns' primary_key flags (a PrimaryKeyConstraint in the constraint set,
// Rev 3, or one a mutation site may add, Rev 3.1): the table's primary_key
// is unknown, every column's absent primary_key: false is unknown, and so is
// K2's nullability derived from it (a PrimaryKeyConstraint makes its columns
// not nullable when the Column does not set nullable).
func (t *table) openPrimaryKey(row, file string, line int, why string) {
	t.pkOpen = true
	for _, col := range t.columns {
		if col.unknown {
			continue
		}
		changed := false
		if col.attrs["primary_key"] == false {
			col.set(row, "primary_key", nil)
			changed = true
		}
		if col.nullableFromPK && col.attrs["nullable"] == true {
			col.set(row, "nullable", nil)
			changed = true
		}
		if changed {
			col.reasons = append(col.reasons, reason{Row: row, Fact: fact("primary_key"), File: file, Line: line, Reason: why})
		}
	}
}

// primaryKeyOpen reports the row that leaves a __table_args__ value open to a
// primary key, or "" when it is a tuple whose elements are all calls of
// UniqueConstraint, Index or CheckConstraint (TA1-TA3), which never set one.
func (l *linker) primaryKeyOpen(file string, scope int, m *facts.PythonModelMember) string {
	v := m.Value
	if v == nil || v.Kind != "tuple" {
		return "M11"
	}
	for i := range v.Args {
		e := &v.Args[i]
		if e.Kind != "call" {
			return "TA-D"
		}
		switch l.canon(file, scope, e) {
		case "sqlalchemy.UniqueConstraint", "sqlalchemy.Index", "sqlalchemy.CheckConstraint":
		default:
			return "TA-D"
		}
	}
	return ""
}

// literalRejected proves M12's P1 form: pydantic rejects a non-annotated,
// non-underscore class attribute whose value is a literal
// (PydanticUserError "A non-annotated attribute was detected"), unless the
// name is a class variable of a base or the namespace binds it otherwise.
// The proof needs the whole chain read: the name bound once in its class,
// annotated in no chain class (a field or ClassVar of that name changes the
// outcome), not one of SQLModel's class variables, no chain class with an
// unread statement (an if block may annotate it) and every chain config
// readable (ignored_types is outside K-cfg).
func (l *linker) literalRejected(chain []*classRec, owner *classRec, name string) bool {
	if sqlmodelClassVars[name] {
		return false
	}
	n := 0
	for _, m := range owner.rec.Members {
		if m.Name == name {
			n++
		}
	}
	if n != 1 {
		return false
	}
	for _, cr := range chain {
		if !l.configReadable(cr.file, cr.rec) {
			return false
		}
		for _, m := range cr.rec.Members {
			if m.Kind == "other" || m.Kind == "annotated" && m.Name == name {
				return false
			}
		}
	}
	return true
}

// sqlmodelClassVars are SQLModel 0.0.39's class variables without a leading
// underscore (SQLModel.__class_vars__); pydantic skips a base's class
// variable, so a literal assigned to one is accepted.
var sqlmodelClassVars = map[string]bool{"metadata": true}

func chainScope(chain []*classRec, file string) int {
	for _, cr := range chain {
		if cr.file == file {
			return cr.rec.Scope
		}
	}
	return 0
}

// unknownIfField adds col (unknown) when a farther chain class defines a
// field of that name (M12: "if the member binds a known name, that column
// is unknown").
func (t *table) unknownIfField(l *linker, chain []*classRec, col *column) {
	for _, cr := range chain {
		for _, m := range cr.rec.Members {
			if m.Kind == "annotated" && m.Name == col.attribute {
				l.addColumn(t, col)
				return
			}
		}
	}
}

func (l *linker) addColumn(t *table, col *column) {
	if col.unknown {
		t.columnsComplete = false
		t.constraintsComplete = false
		t.reasons = append(t.reasons, col.reasons...)
	}
	t.columns = append(t.columns, col)
	t.byName[col.name] = col
}

// tableArgs reads M11 and TA1-TA3, TA-D.
func (l *linker) tableArgs(t *table, file string, scope int, m *facts.PythonModelMember) {
	v := m.Value
	if v == nil || v.Kind != "tuple" {
		t.partial("M11", file, m.Line, "__table_args__ is not a tuple literal", false, true)
		return
	}
	t.schemaKnown = true
	for _, e := range v.Args {
		if e.Kind == "dict" {
			t.schemaKnown = false
			t.partial("TA-D", file, m.Line, "options dict in __table_args__", false, true)
			continue
		}
		if e.Kind != "call" {
			t.partial("TA-D", file, m.Line, "element that is not a constraint call", false, true)
			continue
		}
		switch l.canon(file, scope, &e) {
		case "sqlalchemy.UniqueConstraint":
			tag("TA1")
			cols, ok := strArgs(e.Args)
			name, nameOK := writtenName(l, file, scope, e, "name")
			if !ok || !nameOK || len(e.Keywords) > 1 || len(e.Keywords) == 1 && e.Keywords[0].Name != "name" {
				t.partial("TA-D", file, m.Line, "UniqueConstraint the contract does not read", false, true)
				continue
			}
			t.uniques = append(t.uniques, map[string]any{"name": name, "columns": cols})
		case "sqlalchemy.Index":
			tag("TA2")
			if len(e.Args) == 0 || e.Args[0].Kind != "literal" || e.Args[0].LitKind != "str" {
				t.partial("TA-D", file, m.Line, "Index without a literal name", false, true)
				continue
			}
			ix := map[string]any{"name": e.Args[0].Literal, "unique": false, "columns": nil}
			if cols, ok := strArgs(e.Args[1:]); ok {
				ix["columns"] = cols
			}
			bad := false
			for _, k := range e.Keywords {
				switch {
				case k.Name == "unique":
					if b, ok := litBool(k.Value); ok {
						ix["unique"] = b
					} else {
						bad = true
					}
				case strings.Contains(k.Name, "_") && !strings.HasPrefix(k.Name, "_"):
				default:
					bad = true
				}
			}
			if bad {
				t.partial("TA-D", file, m.Line, "Index keyword the contract does not read", false, true)
				continue
			}
			t.indexes = append(t.indexes, ix)
		case "sqlalchemy.CheckConstraint":
			tag("TA3")
			name, nameOK := writtenName(l, file, scope, e, "name")
			if len(e.Args) != 1 || !nameOK || len(e.Keywords) > 1 || len(e.Keywords) == 1 && e.Keywords[0].Name != "name" {
				t.partial("TA-D", file, m.Line, "CheckConstraint the contract does not read", false, true)
				continue
			}
			ck := map[string]any{"name": name, "sqltext": nil}
			if e.Args[0].Kind == "literal" && e.Args[0].LitKind == "str" {
				ck["sqltext"] = e.Args[0].Literal
			}
			t.checks = append(t.checks, ck)
		default:
			t.partial("TA-D", file, m.Line, "element the contract does not read", false, true)
		}
	}
}

func strArgs(args []facts.PythonModelExpr) ([]string, bool) {
	out := []string{}
	for _, a := range args {
		if a.Kind != "literal" || a.LitKind != "str" {
			return nil, false
		}
		out = append(out, a.Literal)
	}
	return out, true
}

// writtenName reads a name= keyword: absent (nil), a str literal, or
// conv("<literal>") recorded as written.
func writtenName(l *linker, file string, scope int, e facts.PythonModelExpr, key string) (any, bool) {
	v, ok := kw(&e, key)
	if !ok {
		return nil, true
	}
	if v.Kind == "literal" && v.LitKind == "str" {
		return v.Literal, true
	}
	if v.Kind == "call" && len(v.Args) == 1 && v.Args[0].Kind == "literal" && v.Args[0].LitKind == "str" && strings.HasSuffix(l.canon(file, scope, &v), ".conv") {
		return v.Args[0].Literal, true
	}
	return nil, false
}

package sqlmodel

import (
	"fmt"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/testscope"
)

// site applies rows X1, U5 and L8 to one mutation site or DDL hook.
func (l *linker) site(file string, s facts.PythonModelSite) {
	test := testscope.Classify(file, l.in.Tests) == testscope.Test
	var targets []*table
	resolved := false
	switch s.Kind {
	case "constraint_call":
		tag("X1")
		// A list or tuple argument (ForeignKeyConstraint's columns) is read
		// element by element (Rev 3.1).
		var args []facts.PythonModelExpr
		for _, a := range s.Args {
			if a.Kind == "list" || a.Kind == "tuple" {
				args = append(args, a.Args...)
			} else {
				args = append(args, a)
			}
		}
		// Only str literals: the constructor attaches to no table.
		resolved = true
		for _, a := range args {
			if a.Kind == "literal" && a.LitKind == "str" {
				continue
			}
			ts, ok := l.receiver(file, s.Scope, a.Name)
			if !ok {
				resolved = false
				targets = nil
				break
			}
			targets = append(targets, ts...)
		}
	default:
		if s.Kind == "ddl_hook" {
			tag("U5")
		} else {
			tag("X1")
		}
		targets, resolved = l.receiver(file, s.Scope, s.Receiver.Name)
	}
	if !resolved {
		tag("L8")
		targets = nil
		if test {
			return
		}
		for _, t := range l.tables {
			if !t.test {
				targets = append(targets, t)
			}
		}
	}
	kind := l.siteConstraint(file, s)
	for _, t := range targets {
		if test && !t.test {
			continue
		}
		why := fmt.Sprintf("%s at %s:%d", s.Kind, file, s.Line)
		switch s.Kind {
		case "ddl_hook":
			t.ddlHooks = true
		case "append_column":
			t.partial("X1", file, s.Line, why, true, true)
			if mayReplace(s) {
				// replace_existing may replace a column the class defines.
				tag("X1/replace")
				for _, c := range t.columns {
					c.makeUnknown("X1", why+" (may replace a defined column)")
				}
			}
		case "attribute_assign":
			t.partial("X1", file, s.Line, why, true, true)
			target, name := assignTarget(s.Receiver.Name)
			switch target {
			case assignTableIdentity:
				// The table's name, schema or fullname may change (Rev 3.2).
				tag("X1/table_identity")
				if t.refusedWhy == "" {
					t.refusedWhy = fmt.Sprintf("attribute assignment %s at %s:%d may change the table name, schema or fullname", targetText(s.Receiver.Name), file, s.Line)
				}
				continue
			case assignColumnIdentity:
				// The column's name or key may change (Rev 3.2).
				tag("X1/column_identity")
				for _, c := range t.columns {
					if c.name == name {
						c.refused = true
						t.elementsUnknown = true
					}
				}
				continue
			}
			for _, c := range t.columns {
				if name == "" || c.name == name {
					c.makeUnknown("X1", why)
				}
			}
			if name == "" {
				tag("X1/table_attr")
				// A table attribute (schema, primary_key, indexes, ...) may
				// be reassigned: the schema and the listed elements are
				// unknown too.
				t.schemaKnown = false
				t.elementsUnknown = true
				t.pkOpen = true
			}
		case "append_foreign_key":
			// The receiver is a column: its foreign keys gain one.
			tag("X1/FK")
			t.partial("X1", file, s.Line, why, false, true)
			name := receiverColumn(s.Receiver.Name)
			for _, c := range t.columns {
				if name == "" || c.name == name {
					c.openForeignKeys(why)
				}
			}
		default:
			// append_constraint, set_add and constraint_call add a
			// constraint; what it may change depends on its kind (Rev 3.1).
			t.partial("X1", file, s.Line, why, false, true)
			if kind == constraintPrimaryKey || kind == constraintUnknown {
				tag("X1/PK")
				t.openPrimaryKey("X1", file, s.Line, why+" may add a primary key")
			}
			if kind == constraintForeignKey || kind == constraintUnknown {
				tag("X1/FK")
				for _, c := range t.columns {
					c.openForeignKeys(why + " may add a foreign key")
				}
			}
		}
	}
}

// Constraint kinds a mutation site may add (Revision 3.1).
const (
	constraintOther      = "other" // UniqueConstraint, Index, CheckConstraint
	constraintPrimaryKey = "primary_key"
	constraintForeignKey = "foreign_key"
	constraintUnknown    = "unknown"
)

// siteConstraint proves the kind of constraint an append_constraint,
// set_add or constraint_call site adds. Only a resolved
// UniqueConstraint, Index or CheckConstraint call (or constructor) is proven
// to leave the primary key and the foreign keys alone; a constraint held in
// a variable, a splat or any other callee is unknown.
func (l *linker) siteConstraint(file string, s facts.PythonModelSite) string {
	var e *facts.PythonModelExpr
	switch s.Kind {
	case "constraint_call":
		e = &facts.PythonModelExpr{Kind: "call", Name: s.Receiver.Name}
	case "append_constraint", "set_add":
		if len(s.Args) != 1 || len(s.Keywords) != 0 || s.Args[0].Kind != "call" {
			return constraintUnknown
		}
		e = &s.Args[0]
	default:
		return ""
	}
	switch l.canon(file, s.Scope, e) {
	case "sqlalchemy.UniqueConstraint", "sqlalchemy.Index", "sqlalchemy.CheckConstraint":
		return constraintOther
	case "sqlalchemy.PrimaryKeyConstraint":
		return constraintPrimaryKey
	case "sqlalchemy.ForeignKeyConstraint":
		return constraintForeignKey
	}
	return constraintUnknown
}

// mayReplace reports whether an append_column call may replace a defined
// column: any argument besides the column, unless it is
// replace_existing=False (the default raises DuplicateColumnError instead,
// an import failure V7 covers).
func mayReplace(s facts.PythonModelSite) bool {
	if len(s.Args) != 1 {
		return true
	}
	for _, k := range s.Keywords {
		if k.Name != "replace_existing" || k.Value.Kind != "literal" || k.Value.LitKind != "bool" || k.Value.Literal != "False" {
			return true
		}
	}
	return false
}

// What an attribute assignment may change (Rev 3.2).
const (
	assignTableKeep      = "table"           // a table attribute that leaves the table's identity alone
	assignColumnKeep     = "column"          // a column attribute that leaves the column's name and key alone
	assignColumnIdentity = "column_identity" // the column's name or key may change
	assignTableIdentity  = "table_identity"  // the table's name, schema or fullname may change
)

// tableKeepAttrs are the table attributes whose assignment cannot rename
// the table (Rev 3.2); `name`, `schema` and `fullname` can, and any other
// attribute is not proven.
var tableKeepAttrs = map[string]bool{"comment": true, "info": true, "primary_key": true, "indexes": true, "constraints": true, "foreign_keys": true}

// columnKeepAttrs are the column attributes whose assignment cannot change
// the column's name or key (Rev 3.2).
var columnKeepAttrs = map[string]bool{"nullable": true, "type": true, "default": true, "server_default": true, "onupdate": true,
	"server_onupdate": true, "primary_key": true, "index": true, "unique": true, "autoincrement": true, "comment": true,
	"doc": true, "info": true, "foreign_keys": true}

// columnValueAttrs hold values (a type, a default, a dict) that cannot
// reach the column's name or its table, so a longer chain through them
// keeps the column claimed (`c.x.type.length = 5`).
var columnValueAttrs = map[string]bool{"type": true, "info": true, "default": true, "server_default": true, "onupdate": true, "server_onupdate": true}

// assignTarget reads an attribute assignment's target chain (row X1, Rev
// 3.2): which claim it may change and, for a column target, the column it
// names. The chain after `__table__` (or after the head, for a local bound
// to `C.__table__`) decides; a target the capture cannot spell (a
// subscript, so no dotted name) is not proven and may rename the table.
func assignTarget(dotted string) (target, column string) {
	if dotted == "" {
		return assignTableIdentity, ""
	}
	parts := strings.Split(dotted, ".")
	rest := parts[1:]
	for i, p := range parts {
		if p == "__table__" {
			rest = parts[i+1:]
			break
		}
	}
	if len(rest) == 0 {
		return assignTableIdentity, ""
	}
	if (rest[0] != "c" && rest[0] != "columns") || len(rest) < 2 {
		if len(rest) == 1 && tableKeepAttrs[rest[0]] {
			return assignTableKeep, ""
		}
		return assignTableIdentity, ""
	}
	column = rest[1]
	switch {
	case len(rest) == 3 && columnKeepAttrs[rest[2]]:
		return assignColumnKeep, column
	case len(rest) > 3 && columnValueAttrs[rest[2]]:
		return assignColumnKeep, column
	case len(rest) <= 3:
		return assignColumnIdentity, column
	}
	// A chain through another segment (`c.x.table.name`) may reach the table.
	return assignTableIdentity, ""
}

// targetText names an assignment target in a diagnostic.
func targetText(dotted string) string {
	if dotted == "" {
		return "(a target with a subscript)"
	}
	return "`" + dotted + "`"
}

// receiverColumn reads the column a `....c.<col>` receiver names, or "".
func receiverColumn(dotted string) string {
	parts := strings.Split(dotted, ".")
	for i := 1; i+1 < len(parts); i++ {
		if parts[i] == "c" || parts[i] == "columns" {
			return parts[i+1]
		}
	}
	return ""
}

// receiver resolves a site receiver or argument to claimed model tables:
// `C.__table__...`, `C.<attr>`, or a local bound to `C.__table__`. ok is
// false when it does not resolve; a resolution to something that is not a
// model table (a Core Table, a string) is ok with no tables.
func (l *linker) receiver(file string, scope int, dotted string) ([]*table, bool) {
	if dotted == "" {
		return nil, false
	}
	parts := strings.Split(dotted, ".")
	head := parts[0]
	if len(parts) > 1 && parts[1] == "__table__" || len(parts) > 1 && len(parts) <= 3 {
		if ts, ok := l.classTables(file, scope, head); ok {
			return ts, true
		}
	}
	p := l.in.Python[file].Python
	if p == nil {
		return nil, false
	}
	for sc := scope; sc >= 0 && sc < len(p.Scopes); {
		if b, ok := p.Scopes[sc].Bindings[head]; ok {
			if b.Count != 1 || !b.Stable {
				return nil, false
			}
			v := b.Value
			if v.Kind == "attribute" && v.Name == "__table__" && v.Object != nil && v.Object.Kind == "identifier" {
				return l.classTables(file, sc, v.Object.Name)
			}
			return nil, false
		}
		if sc == 0 {
			break
		}
		sc = p.Scopes[sc].Parent
	}
	return nil, false
}

func (l *linker) classTables(file string, scope int, name string) ([]*table, bool) {
	r := l.resolve(file, scope, name)
	if r.class == nil {
		return nil, false
	}
	for _, t := range l.tables {
		if t.file == r.class.file && t.cls.Scope == r.class.scope && t.cls.Name == r.class.name {
			return []*table{t}, true
		}
	}
	// A repository class that is not a claimed table: not a model table we
	// emit, but also not resolved to a table we can scope; stay unresolved.
	return nil, false
}

// Group is the model group of SQLModel tables (T018g0, ADR 0058): tool
// sqlmodel, root "." (the repository root). Every SQLModel table class
// registers in the one SQLModel.metadata (V4), foreign keys resolve across
// the repository (L4) and an unresolved production mutation site affects
// every production table of the repository (L8), so the linker's scope is
// the repository and so is the group's. The facts are SQLAlchemy MetaData
// facts, dialect-independent, checked by the MetaData oracle
// (TestModelOracle): verification oracle, no dialect.
func Group() models.Group {
	return models.Group{Tool: Tool, Root: Root, Pins: Pins, Assumptions: TableAssumptions(), Verification: models.VerificationOracle}
}

// Root is the SQLModel group's root: the repository root.
const Root = "."

// f5SourceHook, when set (tests only), receives each record column's
// identity and the source type F5 mapped its type from (TestModelClaims).
var f5SourceHook func(identity, source string)

// rows names this contract's rows for the core's shared steps.
var rows = models.Rows{Emit: "L3", ForeignKeys: "L4", Overlap: "L5"}

// record converts a claimed table into the core's record; the core then
// resolves its foreign keys (L4), compares it with the migration sets (L5)
// and emits it (L3).
func (t *table) record() *models.Table {
	span := classSpan(t.cls)
	r := &models.Table{
		Identity: t.identity, File: t.file, Name: t.name, Model: t.cls.Name, ModelLine: t.cls.Line,
		ModelIdentity: t.cls.Identity, Span: span, SchemaKnown: t.schemaKnown, Test: t.test,
		ColumnsComplete: t.columnsComplete, ConstraintsComplete: t.constraintsComplete,
		PrimaryKeyOpen: t.pkOpen, ElementsUnknown: t.elementsUnknown, Reasons: t.reasons,
		Uniques: t.uniques, Indexes: t.indexes, Checks: t.checks,
		ByName:     map[string]*models.Column{},
		Attributes: map[string]any{"class": t.cls.Identity, "ddl_hooks": t.ddlHooks},
	}
	byCol := map[*column]*models.Column{}
	for _, c := range t.columns {
		rc := &models.Column{Name: c.name, Attribute: c.attribute, File: c.file, Line: c.line, Unknown: c.unknown, ForeignKeysUnknown: c.fkUnknown, Reasons: c.reasons, Attributes: c.attrs, Claims: map[string]models.ClaimSource{}}
		for f, row := range c.claims {
			rc.Claims[f] = models.ClaimSource{Row: row}
		}
		for _, fk := range c.fks {
			var schema *string
			if fk.hasSchema {
				s := fk.schema
				schema = &s
			}
			rc.ForeignKeys = append(rc.ForeignKeys, &models.ForeignKey{Schema: schema, Table: fk.table, Column: fk.column, OnDelete: fk.onDelete, File: fk.file, Line: fk.line})
		}
		byCol[c] = rc
		r.Columns = append(r.Columns, rc)
		if f5SourceHook != nil && c.f5Source != "" && c.attrs["type"] != nil {
			f5SourceHook(models.ColumnIdentity(r, rc), c.f5Source)
		}
	}
	for name, c := range t.byName {
		r.ByName[name] = byCol[c]
	}
	return r
}

func classSpan(c *facts.PythonModelClass) contract.Span {
	return contract.Span{StartLine: c.Line, StartColumn: 0, EndLine: c.EndLine, EndColumn: 0}
}

package python

import (
	"fmt"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// This file captures the SQLModel/SQLAlchemy model syntax that model linking
// (T018c2) reads: reduced class headers, class bodies, Core table calls and
// post-definition mutation sites. It records syntax only. Every decision that
// needs resolution (is this base SQLModel, is this callee Field) stays at link
// time under the claim contract in docs/lang/python-models.md, whose row ids
// tag each capture branch below (TestModelPayloadContract keeps them 1:1).

// modelTag is a test-only hook: every capture branch reports the contract row
// it serves and the record it produced. It is nil in production.
var modelTag func(row string, record any)

func tagModel(row string, record any) {
	if modelTag != nil {
		modelTag(row, record)
	}
}

const (
	// modelValueDepth bounds reduced values: enough for
	// Field(sa_column=Column(String(64), ForeignKey("t.c", ondelete="CASCADE"))).
	modelValueDepth = 4
	// modelAnnotationDepth bounds reduced annotations (nested generics).
	modelAnnotationDepth = 8
)

// modelCtx carries walk state the model capture needs and the general syntax
// walk does not track.
type modelCtx struct {
	// cond is true inside if/try/for/while/with/match or a function body.
	cond bool
	// args is true inside call arguments or a __table_args__ value, where a
	// constraint constructor belongs to its owner and is not a mutation site.
	args bool
	// owner is the identity of the innermost emitted function, method or
	// class (or module-level constant initialiser), else the file node: the
	// owner of a query site (ADR 0060).
	owner string
}

// modelCompound lists the statements whose bodies make a class conditional.
var modelCompound = map[string]bool{
	"if_statement": true, "elif_clause": true, "else_clause": true, "for_statement": true, "while_statement": true,
	"try_statement": true, "except_clause": true, "except_group_clause": true, "finally_clause": true,
	"with_statement": true, "match_statement": true, "case_clause": true,
}

// modelTypes is the contract's T-list plus AutoString (row T1).
var modelTypes = setOf("Uuid", "String", "Text", "Unicode", "UnicodeText", "Integer", "BigInteger", "SmallInteger", "Boolean", "DateTime", "Date", "Time", "Interval", "Float", "Numeric", "JSON", "LargeBinary", "Enum", "AutoString")

var (
	fieldFlags            = setOf("primary_key", "nullable", "index", "unique")
	fieldMeta             = setOf("description", "title", "examples", "exclude", "include", "repr", "alias", "schema_extra", "json_schema_extra", "ge", "gt", "le", "lt", "min_length", "max_length", "max_digits", "decimal_places")
	fieldValued           = setOf("primary_key", "nullable", "index", "unique", "default", "default_factory", "foreign_key", "ondelete", "sa_type", "sa_column", "max_length", "max_digits", "decimal_places")
	columnValued          = setOf("name", "primary_key", "nullable", "index", "unique", "default", "server_default", "onupdate", "server_onupdate")
	columnMeta            = setOf("comment", "doc", "info", "autoincrement")
	columnItems           = setOf("Sequence", "Identity", "Computed", "FetchedValue", "DefaultClause", "CheckConstraint", "UniqueConstraint", "Index")
	constraintCtor        = setOf("Index", "UniqueConstraint", "CheckConstraint", "ForeignKeyConstraint", "PrimaryKeyConstraint")
	appendMethods         = setOf("append_column", "append_constraint", "append_foreign_key")
	setAttributes         = setOf("indexes", "constraints", "foreign_keys")
	ddlEvents             = setOf("before_create", "after_create", "before_drop", "after_drop")
	declarativeBases      = setOf("DeclarativeBase", "DeclarativeBaseNoMeta", "MappedAsDataclass")
	declarativeDecorators = setOf("mapped", "mapped_as_dataclass", "as_declarative")
)

func setOf(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// lastSegment returns the part of a dotted name after its last dot.
func lastSegment(name string) string {
	return name[strings.LastIndex(name, ".")+1:]
}

// modelGate reports gate G1: the file has an absolute import whose module's
// first dotted segment is sqlmodel or sqlalchemy. Relative imports never pass.
func modelGate(imports []facts.Import) bool {
	for _, imp := range imports {
		first := strings.SplitN(imp.Module, ".", 2)[0]
		if !strings.HasPrefix(imp.Module, ".") && (first == "sqlmodel" || first == "sqlalchemy") {
			return true
		}
	}
	return false
}

// modelCapture accumulates one file's payload during the syntax walk.
type modelCapture struct {
	file   string
	models facts.PythonModels
	nodes  []*parse.Node // class_definition node of each class record
	diags  *[]contract.Diagnostic
}

func newModelCapture(file string, imports []facts.Import, diags *[]contract.Diagnostic) *modelCapture {
	if !modelGate(imports) {
		return nil
	}
	return &modelCapture{file: file, models: facts.PythonModels{Version: facts.PythonModelsVersion}, diags: diags}
}

// class records a class header (gate G2) when it has a base, a keyword or a
// decorator; members are captured after the walk, when gate G3 is known.
func (m *modelCapture) class(n *parse.Node, decorators []*parse.Node, scope int, identity string, ctx modelCtx) {
	c := facts.PythonModelClass{Name: facts.Text(n.Fields["name"]), Identity: identity, Scope: scope, Line: int(n.Row) + 1, EndLine: int(n.EndRow) + 1, Conditional: ctx.cond}
	if supers := n.Fields["superclasses"]; supers != nil {
		for _, a := range supers.Children {
			switch a.Kind {
			case "comment":
			case "keyword_argument":
				v := reduceModel(a.Fields["value"], 1, modelValueDepth)
				if v.Kind != "literal" {
					v = facts.PythonModelExpr{Kind: "other"}
				}
				c.Keywords = append(c.Keywords, facts.PythonModelKeyword{Name: facts.Text(a.Fields["name"]), Value: v})
			case "dictionary_splat":
				c.Keywords = append(c.Keywords, facts.PythonModelKeyword{Value: facts.PythonModelExpr{Kind: "other"}})
			default:
				c.Bases = append(c.Bases, nameOnly(a))
			}
		}
	}
	for _, d := range decorators {
		c.Decorators = append(c.Decorators, decoratorExpr(d))
	}
	if len(c.Bases)+len(c.Keywords)+len(c.Decorators) == 0 {
		return
	}
	m.models.Classes = append(m.models.Classes, c)
	m.nodes = append(m.nodes, n)
}

// decoratorExpr keeps a decorator's dotted name (a call keeps its callee only).
func decoratorExpr(d *parse.Node) facts.PythonModelExpr {
	for _, c := range d.Children {
		if c.Kind != "comment" {
			return nameOnly(c)
		}
	}
	return facts.PythonModelExpr{Kind: "other"}
}

// nameOnly reduces a base or decorator to its dotted name: a name, a call's
// callee or a subscript's base, never arguments.
func nameOnly(n *parse.Node) facts.PythonModelExpr {
	n = unwrapModel(n)
	switch n.Kind {
	case "identifier", "attribute":
		if name := dottedModel(n); name != "" {
			return facts.PythonModelExpr{Kind: "name", Name: name}
		}
	case "call":
		return facts.PythonModelExpr{Kind: "call", Name: dottedModel(n.Fields["function"])}
	case "subscript":
		return facts.PythonModelExpr{Kind: "subscript", Name: dottedModel(n.Fields["value"])}
	case "generic_type":
		if len(n.Children) > 0 {
			return facts.PythonModelExpr{Kind: "subscript", Name: dottedModel(n.Children[0])}
		}
	}
	return facts.PythonModelExpr{Kind: "other"}
}

// call records gates G4 (Core tables) and G5 (mutation sites, DDL hooks).
func (m *modelCapture) call(n *parse.Node, scope int, ctx modelCtx) {
	callee := n.Fields["function"]
	name := dottedModel(callee)
	last := lastSegment(name)
	if callee != nil && callee.Kind == "attribute" {
		last = facts.Text(callee.Fields["attribute"])
	}
	line := int(n.Row) + 1
	args := callArgs(n)
	switch {
	case last == "Table" || last == "map_imperatively":
		m.models.CoreTables = append(m.models.CoreTables, line)
		if last == "Table" {
			tagModel("U3", line)
		} else {
			tagModel("U4", line)
		}
		return
	case name != "" && constraintCtor[last] && !ctx.args:
		var reduced []facts.PythonModelExpr
		literalOnly := true
		for _, a := range args {
			if a.Kind == "keyword_argument" || a.Kind == "dictionary_splat" {
				continue
			}
			r := reduceModel(a, 1, modelValueDepth)
			if r.Kind != "literal" || r.LitKind != "str" {
				literalOnly = false
			}
			reduced = append(reduced, r)
		}
		if !literalOnly {
			m.site(facts.PythonModelSite{Kind: "constraint_call", Line: line, Scope: scope, Receiver: facts.PythonModelExpr{Kind: "name", Name: name}, Args: reduced})
		}
		return
	case (last == "listen" || last == "listens_for") && len(args) >= 2:
		if event, ok := literal(args[1]); ok && ddlEvents[event] {
			m.site(facts.PythonModelSite{Kind: "ddl_hook", Line: line, Scope: scope, Receiver: reduceModel(args[0], 1, modelValueDepth), Args: []facts.PythonModelExpr{{Kind: "literal", LitKind: "str", Literal: event}}})
		}
		return
	}
	if callee == nil || callee.Kind != "attribute" {
		return
	}
	method := facts.Text(callee.Fields["attribute"])
	object := callee.Fields["object"]
	if appendMethods[method] {
		pos, kws := siteArgs(args)
		m.site(facts.PythonModelSite{Kind: method, Line: line, Scope: scope, Receiver: reduceModel(object, 1, modelValueDepth), Args: pos, Keywords: kws})
	} else if method == "add" && object != nil && object.Kind == "attribute" && setAttributes[facts.Text(object.Fields["attribute"])] {
		pos, kws := siteArgs(args)
		m.site(facts.PythonModelSite{Kind: "set_add", Line: line, Scope: scope, Receiver: reduceModel(object, 1, modelValueDepth), Args: pos, Keywords: kws})
	}
}

// siteArgs reduces the arguments of an append or set-add call one level
// deep: a call keeps its callee (the constraint kind, Revision 3.1) and a
// keyword keeps a literal value (append_column's replace_existing); deeper
// parts are kind other, so the payload stays small.
func siteArgs(args []*parse.Node) ([]facts.PythonModelExpr, []facts.PythonModelKeyword) {
	var pos []facts.PythonModelExpr
	var kws []facts.PythonModelKeyword
	for _, a := range args {
		switch a.Kind {
		case "keyword_argument":
			kws = append(kws, facts.PythonModelKeyword{Name: facts.Text(a.Fields["name"]), Value: reduceModel(a.Fields["value"], 1, 1)})
		case "dictionary_splat":
			kws = append(kws, facts.PythonModelKeyword{Value: facts.PythonModelExpr{Kind: "other"}})
		case "list_splat":
			pos = append(pos, facts.PythonModelExpr{Kind: "other"})
		default:
			pos = append(pos, reduceModel(a, 1, 1))
		}
	}
	return pos, kws
}

// assign records attribute assignments on __table__, .c. or .columns. chains.
func (m *modelCapture) assign(n *parse.Node, scope int) {
	left := n.Fields["left"]
	if left == nil || left.Kind != "attribute" {
		return
	}
	segments := attributeSegments(left)
	hit := false
	for i, s := range segments {
		if s == "__table__" || i < len(segments)-1 && i > 0 && (s == "c" || s == "columns") {
			hit = true
		}
	}
	if hit {
		m.site(facts.PythonModelSite{Kind: "attribute_assign", Line: int(n.Row) + 1, Scope: scope, Receiver: reduceModel(left, 1, modelValueDepth)})
	}
}

func (m *modelCapture) site(s facts.PythonModelSite) {
	m.models.Sites = append(m.models.Sites, s)
	if s.Kind == "ddl_hook" {
		tagModel("U5", s)
	} else {
		tagModel("X1", s)
	}
}

// attributeSegments lists the attribute names of a target chain, looking
// through subscripts and calls (T.__table__.c["x"].nullable).
func attributeSegments(n *parse.Node) []string {
	var out []string
	for n != nil {
		switch n.Kind {
		case "attribute":
			out = append([]string{facts.Text(n.Fields["attribute"])}, out...)
			n = n.Fields["object"]
		case "subscript":
			n = n.Fields["value"]
		case "call":
			n = n.Fields["function"]
		case "identifier":
			return append([]string{n.Text}, out...)
		default:
			return out
		}
	}
	return out
}

// finish applies gate G3 and captures members, then returns the payload.
func (m *modelCapture) finish() *facts.PythonModels {
	byName := map[string][]int{}
	for i, c := range m.models.Classes {
		byName[c.Name] = append(byName[c.Name], i)
	}
	captured := make([]bool, len(m.models.Classes))
	chain := make([]bool, len(m.models.Classes))
	var queue []int
	for i, c := range m.models.Classes {
		table := false
		for _, k := range c.Keywords {
			table = table || k.Name == "table"
		}
		sqlmodel := false
		for _, b := range c.Bases {
			sqlmodel = sqlmodel || b.Name != "" && lastSegment(b.Name) == "SQLModel"
		}
		if table || sqlmodel {
			captured[i] = true
		}
	}
	// (Rev 3) A same-file subclass of a model class is a model class too
	// (`IDMixin(TimestampMixin)` where `TimestampMixin(SQLModel)`): a table
	// class in another file may name it as its base. Downward fixpoint over
	// the classes captured by the rule above and by this one; the upward
	// walk below then adds their bases.
	for changed := true; changed; {
		changed = false
		for i, c := range m.models.Classes {
			if captured[i] {
				continue
			}
			for _, b := range c.Bases {
				if b.Name == "" {
					continue
				}
				for _, j := range byName[lastSegment(b.Name)] {
					if captured[j] && !captured[i] {
						captured[i] = true
						changed = true
					}
				}
			}
		}
	}
	for i := range captured {
		if captured[i] {
			queue = append(queue, i)
		}
	}
	for len(queue) > 0 {
		i := queue[0]
		queue = queue[1:]
		for _, b := range m.models.Classes[i].Bases {
			if b.Name == "" {
				continue
			}
			for _, j := range byName[lastSegment(b.Name)] {
				chain[j] = true
				if !captured[j] {
					captured[j] = true
					queue = append(queue, j)
				}
			}
		}
	}
	tables := map[string]bool{}
	for _, c := range m.models.Classes {
		for _, k := range c.Keywords {
			if k.Name == "table" && k.Value.LitKind == "bool" && k.Value.Literal == "True" {
				tables[c.Name] = true
			}
		}
	}
	var kept []facts.PythonModelClass
	for i := range m.models.Classes {
		c, ok := m.captureClass(m.models.Classes[i], m.nodes[i], captured[i])
		if !ok {
			continue
		}
		m.tagClass(c, chain[i], tables)
		kept = append(kept, c)
	}
	m.models.Classes = kept
	return &m.models
}

// captureClass fills the members of one class. A failure drops that class's
// record and becomes a file diagnostic; the file's other facts are kept.
func (m *modelCapture) captureClass(c facts.PythonModelClass, n *parse.Node, captured bool) (out facts.PythonModelClass, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			*m.diags = append(*m.diags, contract.Diagnostic{Type: "diagnostic", File: m.file, Severity: "warning", Code: "python_model_capture_failed", Message: fmt.Sprintf("model syntax of class %s not captured: %v", c.Name, r)})
			ok = false
		}
	}()
	if captured {
		c.MembersCaptured = true
		if body := n.Fields["body"]; body != nil {
			for _, s := range body.Children {
				if member, keep := classMember(s); keep {
					c.Members = append(c.Members, member)
				}
			}
		}
	}
	return c, true
}

// tagClass reports the class-level contract rows a header record serves.
func (m *modelCapture) tagClass(c facts.PythonModelClass, chain bool, tables map[string]bool) {
	var table *facts.PythonModelKeyword
	other := false
	for i, k := range c.Keywords {
		if k.Name == "table" {
			table = &c.Keywords[i]
		} else {
			other = true
		}
	}
	switch {
	case table == nil || table.Value.LitKind == "bool" && table.Value.Literal == "False":
		tagModel("C1b", c)
	case table.Value.LitKind == "bool" && table.Value.Literal == "True":
		tagModel("C1", c)
	default:
		tagModel("C1c", c)
	}
	if chain {
		tagModel("C2", c)
	}
	declarative := false
	for _, b := range c.Bases {
		if b.Name != "" && tables[lastSegment(b.Name)] {
			tagModel("C3", c)
		}
		declarative = declarative || b.Name != "" && declarativeBases[lastSegment(b.Name)]
	}
	for _, d := range c.Decorators {
		declarative = declarative || d.Name != "" && declarativeDecorators[lastSegment(d.Name)]
	}
	if declarative {
		tagModel("U1", c)
	}
	if other {
		tagModel("C4", c)
	}
	if len(c.Bases) != 1 {
		tagModel("C6", c)
	}
	if c.MembersCaptured {
		named := false
		for _, mem := range c.Members {
			named = named || mem.Name == "__tablename__"
		}
		if !named {
			tagModel("N2", c)
		}
	}
}

// classMember reduces one class-body statement (rows M0-M12, N1, U2, D).
// M0 statements (docstrings, pass, ...) are dropped.
func classMember(s *parse.Node) (facts.PythonModelMember, bool) {
	line := int(s.Row) + 1
	other := func() (facts.PythonModelMember, bool) {
		m := facts.PythonModelMember{Kind: "other", Line: line, Detail: s.Kind}
		tagModel("M12", m)
		return m, true
	}
	switch s.Kind {
	case "comment", "pass_statement":
		return facts.PythonModelMember{}, false
	case "function_definition":
		return method(s, nil), true
	case "decorated_definition":
		def := s.Fields["definition"]
		var decorators []*parse.Node
		for _, c := range s.Children {
			if c.Kind == "decorator" {
				decorators = append(decorators, c)
			}
		}
		if def != nil && def.Kind == "function_definition" {
			return method(def, decorators), true
		}
		if def != nil && def.Kind == "class_definition" {
			return nestedClass(def), true
		}
		return other()
	case "class_definition":
		return nestedClass(s), true
	case "expression_statement":
		var parts []*parse.Node
		for _, c := range s.Children {
			if c.Kind != "comment" {
				parts = append(parts, c)
			}
		}
		if len(parts) != 1 {
			return other()
		}
		switch parts[0].Kind {
		case "string", "concatenated_string", "ellipsis":
			return facts.PythonModelMember{}, false
		case "assignment":
			return assignment(parts[0], line, other)
		}
		return other()
	}
	return other()
}

func method(def *parse.Node, decorators []*parse.Node) facts.PythonModelMember {
	m := facts.PythonModelMember{Kind: "method", Name: facts.Text(def.Fields["name"]), Line: int(def.Row) + 1}
	declared := false
	for _, d := range decorators {
		e := decoratorExpr(d)
		declared = declared || lastSegment(e.Name) == "declared_attr"
		m.Decorators = append(m.Decorators, e)
	}
	tagModel("M1", m)
	if declared {
		tagModel("U2", m)
	}
	return m
}

// nestedClass keeps a nested class Config with its key = literal members
// (row M9); any other nested class keeps its kind, name and line (M12).
func nestedClass(def *parse.Node) facts.PythonModelMember {
	m := facts.PythonModelMember{Kind: "class", Name: facts.Text(def.Fields["name"]), Line: int(def.Row) + 1}
	if m.Name != "Config" {
		tagModel("M12", m)
		return m
	}
	if body := def.Fields["body"]; body != nil {
		for _, s := range body.Children {
			line := int(s.Row) + 1
			switch s.Kind {
			case "comment", "pass_statement":
				continue
			case "expression_statement":
				if len(s.Children) == 1 && s.Children[0].Kind == "assignment" {
					a := s.Children[0]
					left, right := a.Fields["left"], a.Fields["right"]
					if left != nil && left.Kind == "identifier" && right != nil && a.Fields["type"] == nil {
						v := reduceModel(right, 1, modelValueDepth)
						m.Members = append(m.Members, facts.PythonModelMember{Kind: "assign", Name: left.Text, Line: line, Value: &v})
						continue
					}
				}
				if len(s.Children) == 1 && (s.Children[0].Kind == "string" || s.Children[0].Kind == "concatenated_string") {
					continue
				}
			}
			m.Members = append(m.Members, facts.PythonModelMember{Kind: "other", Line: line, Detail: s.Kind})
		}
	}
	tagModel("M9", m)
	tagModel("C5", m)
	return m
}

// assignment reduces an annotated or plain assignment in a class body.
func assignment(a *parse.Node, line int, other func() (facts.PythonModelMember, bool)) (facts.PythonModelMember, bool) {
	left, right, annotation := a.Fields["left"], a.Fields["right"], a.Fields["type"]
	if left == nil || left.Kind != "identifier" || right != nil && right.Kind == "assignment" {
		return other()
	}
	name := left.Text
	dunder := len(name) > 4 && strings.HasPrefix(name, "__") && strings.HasSuffix(name, "__")
	private := strings.HasPrefix(name, "_") && !dunder
	if annotation == nil {
		m := facts.PythonModelMember{Kind: "assign", Name: name, Line: line}
		switch {
		case private:
			m.Line = 0
			tagModel("M7", m)
		case name == "__tablename__":
			v := reduceModel(right, 1, modelValueDepth)
			m.Value = &v
			tagModel("N1", m)
		case name == "__table_args__":
			v := tableArgs(right)
			m.Value = &v
			tagModel("M11", m)
		case name == "__mapper_args__":
			v := reduceModel(right, 1, modelValueDepth)
			m.Value = &v
			tagModel("M10", m)
		case name == "model_config":
			v := reduceModel(right, 1, modelValueDepth)
			m.Value = &v
			tagModel("M8", m)
			tagModel("C5", m)
		case name == "__table__":
			tagModel("U2", m)
		default:
			// (Rev 3) A literal value is kept: pydantic rejects a
			// non-annotated literal class attribute, a proven failure (L7).
			if right != nil {
				if v := reduceModel(right, 1, modelValueDepth); v.Kind == "literal" {
					m.Value = &v
				}
			}
			tagModel("M12", m)
		}
		return m, true
	}
	ann := reduceModel(annotation, 1, modelAnnotationDepth)
	ann.Text = facts.CompactPython(annotation.Text)
	m := facts.PythonModelMember{Kind: "annotated", Name: name, Line: line}
	if private {
		m.Line = 0
		tagModel("M7", m)
		return m, true
	}
	if ann.Kind == "subscript" && lastSegment(ann.Name) == "ClassVar" {
		m.Annotation = &ann
		tagModel("M6", m)
		return m, true
	}
	var value *facts.PythonModelExpr
	callee := ""
	if right != nil {
		r := unwrapModel(right)
		if r.Kind == "call" {
			callee = lastSegment(dottedModel(r.Fields["function"]))
		}
		if callee == "Relationship" {
			value = &facts.PythonModelExpr{Kind: "call", Name: dottedModel(r.Fields["function"])}
			m.Value = value
			tagModel("M5", m)
			return m, true
		}
		v := reduceModel(right, 1, modelValueDepth)
		if name == "__table_args__" {
			v = tableArgs(right)
		}
		value = &v
	}
	m.Annotation = &ann
	m.Value = value
	switch {
	case name == "__tablename__":
		tagModel("N1", m)
	case name == "__table_args__":
		tagModel("M11", m)
	case dunder:
		tagModel("M12", m)
	case value == nil:
		tagModel("M3", m)
	case callee == "Field":
		tagModel("M2", m)
		fieldTags(unwrapModel(right), *value)
	case value.Kind == "literal" && value.LitKind != "bytes" && value.LitKind != "ellipsis":
		tagModel("M4", m)
	default:
		tagModel("D", m)
	}
	if ann.Kind == "subscript" && lastSegment(ann.Name) == "Mapped" || callee == "mapped_column" || callee == "relationship" {
		tagModel("U2", m)
	}
	if !dunder {
		annotationTags(ann)
		if callee != "Field" || !hasKeyword(*value, "sa_type") && !hasKeyword(*value, "sa_column") {
			tagModel("F5", ann)
		}
	}
	return m, true
}

func hasKeyword(e facts.PythonModelExpr, name string) bool {
	for _, k := range e.Keywords {
		if k.Name == name {
			return true
		}
	}
	return false
}

// annotationTags reports the syntactic annotation rows (A1, A2, A3, A1m).
// Resolution (A2 versus A3 for names, A5 aliases) happens at link time.
func annotationTags(ann facts.PythonModelExpr) {
	inner := ann
	if ann.Kind == "subscript" && lastSegment(ann.Name) == "Annotated" {
		tagModel("A1m", ann)
		if len(ann.Args) > 0 {
			inner = ann.Args[0]
		}
	}
	switch {
	case inner.Kind == "literal" && inner.LitKind == "str":
		tagModel("A3", inner)
	case optionalAnnotation(inner):
		tagModel("A1", inner)
	default:
		tagModel("A2", inner)
	}
}

// optionalAnnotation is the syntactic shape of A1: a union with a None member.
func optionalAnnotation(e facts.PythonModelExpr) bool {
	if e.Kind == "subscript" && lastSegment(e.Name) == "Optional" {
		return true
	}
	if e.Kind == "union" || e.Kind == "subscript" && lastSegment(e.Name) == "Union" {
		for _, a := range e.Args {
			if a.Kind == "literal" && a.LitKind == "none" {
				return true
			}
		}
	}
	return false
}

// fieldTags reports the F rows of a Field call and the K rows of its
// sa_column=Column(...) argument.
func fieldTags(call *parse.Node, e facts.PythonModelExpr) {
	flags := false
	saColumn := false
	for _, a := range e.Args {
		tagModel("F3", a)
	}
	for _, k := range e.Keywords {
		switch {
		case fieldFlags[k.Name]:
			tagModel("F1", k)
			flags = true
		case k.Name == "default":
			tagModel("F3", k)
		case k.Name == "default_factory":
			tagModel("F3b", k)
		case k.Name == "foreign_key" || k.Name == "ondelete":
			tagModel("F4", k)
			flags = true
		case k.Name == "sa_type":
			tagModel("F6", k)
			typeTags(k.Value, "F6")
			flags = true
		case k.Name == "sa_column":
			tagModel("F7", k)
			saColumn = true
			if k.Value.Kind == "call" && lastSegment(k.Value.Name) == "Column" {
				columnTags(k.Value)
			}
		case k.Name == "sa_column_args" || k.Name == "sa_column_kwargs":
			tagModel("F8", k)
			flags = true
		case fieldMeta[k.Name]:
			tagModel("F9", k)
		default:
			tagModel("D", k)
		}
	}
	if saColumn && flags {
		tagModel("F7r", e)
	}
}

// typeTags reports T1 for a type expression whose name is in the T-list and
// K1b for any other expression in a type position.
func typeTags(e facts.PythonModelExpr, fallback string) {
	if (e.Kind == "name" || e.Kind == "call") && modelTypes[lastSegment(e.Name)] {
		tagModel("T1", e)
	} else if fallback == "K1b" {
		tagModel("K1b", e)
	}
}

// columnTags classifies the arguments of a reduced Column(...) call by their
// own form (PF1): an optional name, an optional type, then schema items.
func columnTags(e facts.PythonModelExpr) {
	tagModel("K1", e)
	typed := false
	for i, a := range e.Args {
		last := lastSegment(a.Name)
		switch {
		case i == 0 && a.Kind == "literal" && a.LitKind == "str":
		case a.Kind == "call" && last == "ForeignKey":
			tagModel("K3", a)
		case (a.Kind == "call" || a.Kind == "name") && columnItems[last]:
			tagModel("K6", a)
		case !typed:
			typed = true
			typeTags(a, "K1b")
		default:
			tagModel("K6", a)
		}
	}
	for _, k := range e.Keywords {
		switch {
		case k.Name == "name":
			tagModel("K1", k)
		case fieldFlags[k.Name]:
			tagModel("K2", k)
		case columnValued[k.Name]:
			tagModel("K4", k)
		case columnMeta[k.Name]:
			tagModel("K5", k)
		default:
			tagModel("K6", k)
		}
	}
}

// tableArgs reduces a __table_args__ value: tuple elements keep their call
// arguments (rows TA1-TA3); any other element keeps its kind only (TA-D).
func tableArgs(n *parse.Node) facts.PythonModelExpr {
	v := reduceModel(n, 1, modelValueDepth)
	if v.Kind != "tuple" {
		return v
	}
	for i, el := range v.Args {
		switch {
		case el.Kind == "call" && lastSegment(el.Name) == "UniqueConstraint":
			tagModel("TA1", el)
		case el.Kind == "call" && lastSegment(el.Name) == "Index":
			tagModel("TA2", el)
		case el.Kind == "call" && lastSegment(el.Name) == "CheckConstraint":
			tagModel("TA3", el)
		default:
			v.Args[i] = facts.PythonModelExpr{Kind: el.Kind, Name: el.Name}
			tagModel("TA-D", v.Args[i])
		}
	}
	return v
}

// unwrapModel looks through annotation wrappers and single parentheses.
func unwrapModel(n *parse.Node) *parse.Node {
	for n != nil && (n.Kind == "type" || n.Kind == "parenthesized_expression") && len(n.Children) == 1 {
		n = n.Children[0]
	}
	return n
}

// dottedModel returns a.b.c for a chain of identifiers, otherwise "".
func dottedModel(n *parse.Node) string {
	n = unwrapModel(n)
	if n == nil {
		return ""
	}
	switch n.Kind {
	case "identifier":
		return n.Text
	case "attribute":
		object := dottedModel(n.Fields["object"])
		if object == "" {
			return ""
		}
		return object + "." + facts.Text(n.Fields["attribute"])
	}
	return ""
}

// callArgs lists a call's argument nodes without comments.
func callArgs(n *parse.Node) []*parse.Node {
	var out []*parse.Node
	if args := n.Fields["arguments"]; args != nil {
		for _, a := range args.Children {
			if a.Kind != "comment" {
				out = append(out, a)
			}
		}
	}
	return out
}

// reduceModel reduces an expression or annotation to depth limit; deeper or
// unlisted forms are kind other with no text. Field and Column calls keep
// values only for the keywords the contract reads (names otherwise), and a
// Relationship call keeps its callee only.
func reduceModel(n *parse.Node, depth, limit int) facts.PythonModelExpr {
	n = unwrapModel(n)
	if n == nil || depth > limit {
		return facts.PythonModelExpr{Kind: "other"}
	}
	switch n.Kind {
	case "string", "concatenated_string":
		if s, ok := literal(n); ok {
			return facts.PythonModelExpr{Kind: "literal", LitKind: "str", Literal: s}
		}
		if i := strings.IndexAny(n.Text, "\"'"); i > 0 && strings.ContainsAny(n.Text[:i], "bB") {
			return facts.PythonModelExpr{Kind: "literal", LitKind: "bytes"}
		}
	case "integer":
		return facts.PythonModelExpr{Kind: "literal", LitKind: "int", Literal: n.Text}
	case "float":
		return facts.PythonModelExpr{Kind: "literal", LitKind: "float", Literal: n.Text}
	case "true", "false":
		return facts.PythonModelExpr{Kind: "literal", LitKind: "bool", Literal: n.Text}
	case "none":
		return facts.PythonModelExpr{Kind: "literal", LitKind: "none", Literal: "None"}
	case "ellipsis":
		return facts.PythonModelExpr{Kind: "literal", LitKind: "ellipsis", Literal: "..."}
	case "unary_operator":
		arg := unwrapModel(n.Fields["argument"])
		op := strings.TrimSpace(strings.TrimSuffix(n.Text, facts.Text(arg)))
		if arg != nil && (arg.Kind == "integer" || arg.Kind == "float") && (op == "-" || op == "+") {
			kind := "int"
			if arg.Kind == "float" {
				kind = "float"
			}
			return facts.PythonModelExpr{Kind: "literal", LitKind: kind, Literal: op + arg.Text}
		}
	case "identifier", "attribute":
		if name := dottedModel(n); name != "" {
			return facts.PythonModelExpr{Kind: "name", Name: name}
		}
	case "call":
		return reduceCall(n, depth, limit)
	case "subscript":
		e := facts.PythonModelExpr{Kind: "subscript", Name: dottedModel(n.Fields["value"])}
		for _, c := range n.Children[1:] {
			if c.Kind != "comment" {
				e.Args = append(e.Args, reduceModel(c, depth+1, limit))
			}
		}
		return e
	case "generic_type":
		if len(n.Children) == 0 {
			break
		}
		e := facts.PythonModelExpr{Kind: "subscript", Name: dottedModel(n.Children[0])}
		for _, c := range n.Children[1:] {
			if c.Kind == "type_parameter" {
				for _, p := range c.Children {
					if p.Kind != "comment" {
						e.Args = append(e.Args, reduceModel(p, depth+1, limit))
					}
				}
			}
		}
		return e
	case "binary_operator", "union_type":
		if n.Kind == "binary_operator" && !unionOperator(n) {
			break
		}
		e := facts.PythonModelExpr{Kind: "union"}
		var flatten func(*parse.Node)
		flatten = func(c *parse.Node) {
			c = unwrapModel(c)
			if c != nil && (c.Kind == "union_type" || c.Kind == "binary_operator" && unionOperator(c)) {
				for _, o := range c.Children {
					if o.Kind != "comment" {
						flatten(o)
					}
				}
				return
			}
			e.Args = append(e.Args, reduceModel(c, depth+1, limit))
		}
		flatten(n)
		return e
	case "tuple", "list", "expression_list":
		kind := "tuple"
		if n.Kind == "list" {
			kind = "list"
		}
		e := facts.PythonModelExpr{Kind: kind}
		for _, c := range n.Children {
			if c.Kind != "comment" {
				e.Args = append(e.Args, reduceModel(c, depth+1, limit))
			}
		}
		return e
	case "dictionary":
		e := facts.PythonModelExpr{Kind: "dict"}
		for _, c := range n.Children {
			switch c.Kind {
			case "comment":
			case "pair":
				e.Keys = append(e.Keys, reduceModel(c.Fields["key"], depth+1, limit))
				e.Args = append(e.Args, reduceModel(c.Fields["value"], depth+1, limit))
			default:
				e.Keys = append(e.Keys, facts.PythonModelExpr{Kind: "other"})
				e.Args = append(e.Args, facts.PythonModelExpr{Kind: "other"})
			}
		}
		return e
	case "lambda":
		return facts.PythonModelExpr{Kind: "lambda"}
	}
	return facts.PythonModelExpr{Kind: "other"}
}

// unionOperator reports a binary_operator whose operator is |. The grammar
// keeps the operator token out of the detached children, so it is read from
// the text between the operands.
func unionOperator(n *parse.Node) bool {
	left, right := n.Fields["left"], n.Fields["right"]
	if n.Kind != "binary_operator" || left == nil || right == nil || left.End < n.Start || right.Start < left.End || int(right.Start-n.Start) > len(n.Text) {
		return false
	}
	return strings.TrimSpace(n.Text[left.End-n.Start:right.Start-n.Start]) == "|"
}

func reduceCall(n *parse.Node, depth, limit int) facts.PythonModelExpr {
	name := dottedModel(n.Fields["function"])
	e := facts.PythonModelExpr{Kind: "call", Name: name}
	last := lastSegment(name)
	if name != "" && last == "Relationship" {
		return e
	}
	for _, a := range callArgs(n) {
		switch a.Kind {
		case "keyword_argument":
			key := facts.Text(a.Fields["name"])
			value := facts.PythonModelExpr{Kind: "other"}
			if name == "" || keepKeyword(last, key) {
				value = reduceModel(a.Fields["value"], depth+1, limit)
			}
			if key == "sa_type" && last == "Field" && a.Fields["value"] != nil {
				value.Text = facts.CompactPython(a.Fields["value"].Text)
			}
			e.Keywords = append(e.Keywords, facts.PythonModelKeyword{Name: key, Value: value})
		case "dictionary_splat":
			e.Keywords = append(e.Keywords, facts.PythonModelKeyword{Value: facts.PythonModelExpr{Kind: "other"}})
		case "list_splat":
			e.Args = append(e.Args, facts.PythonModelExpr{Kind: "other"})
		default:
			e.Args = append(e.Args, reduceModel(a, depth+1, limit))
		}
	}
	if last == "Column" {
		// The type position of a Column keeps its text for the written-type display.
		for i, a := range callArgs(n) {
			if a.Kind == "keyword_argument" || a.Kind == "dictionary_splat" || a.Kind == "list_splat" || i >= len(e.Args) {
				break
			}
			arg := e.Args[i]
			if i == 0 && arg.Kind == "literal" && arg.LitKind == "str" || arg.Kind == "call" && (lastSegment(arg.Name) == "ForeignKey" || columnItems[lastSegment(arg.Name)]) {
				continue
			}
			if arg.Kind == "name" || arg.Kind == "call" {
				e.Args[i].Text = facts.CompactPython(a.Text)
			}
			break
		}
	}
	return e
}

// keepKeyword reports whether a keyword value is captured: Field and Column
// keep the values the contract reads, Index keeps unique, others keep all.
func keepKeyword(callee, key string) bool {
	switch callee {
	case "Field":
		return fieldValued[key]
	case "Column":
		return columnValued[key]
	case "Index":
		return key == "unique"
	}
	return true
}

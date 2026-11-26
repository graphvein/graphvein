package facts

import "github.com/graphvein/graphvein/internal/contract"

// PythonFacts retains syntax only; module and route links use each current scan.
type PythonFacts struct {
	Scopes      []PythonScope
	Routes      []PythonRoute
	HelperCalls []PythonHelperCall
	// Models is the SQLModel/SQLAlchemy model syntax of a file that imports
	// sqlmodel or sqlalchemy (ADR 0053); absent for every other file, so
	// their cached facts do not change.
	Models *PythonModels `json:",omitempty"`
	// Rejected marks a file the reference Python rejects (row PS-F2 of
	// docs/lang/python-structure.md): its structure claims nothing beyond
	// the file node, and the linker emits no import edge for it.
	Rejected bool `json:",omitempty"`
	// Unread counts the structural forms of the file that went to the
	// default branch D, by form; the linker adds its own and emits the
	// python_structure_unread diagnostic.
	Unread map[string]int `json:",omitempty"`
	// FastAPIDependencies reports that the file imports fastapi (gate DG of
	// docs/extract/auth.md), so its function scopes carry Dependencies and
	// its module-level Annotated aliases keep their arguments. A dependency
	// function in a file without it has unread parameters (rows DA-F1,
	// DA-F2: unknown, never "no dependency").
	FastAPIDependencies bool `json:",omitempty"`
	// Shape is the FastAPI route shape syntax (gate SG, see PythonShape);
	// absent for files that import none of fastapi, starlette, pydantic
	// and sqlmodel.
	Shape *PythonShape `json:",omitempty"`
}

// PythonScope preserves function locals independently of conditional blocks.
type PythonScope struct {
	Parent     int
	Name       string
	Class      bool
	Bindings   map[string]PythonBinding
	Helper     *PythonHelper
	Parameters []string
	// Dependencies are the parameters of a function scope whose default or
	// annotation can declare a FastAPI dependency (a call default, an
	// annotation), in declaration order; only in files that pass gate DG.
	Dependencies []PythonDependencyParameter `json:",omitempty"`
}

// PythonDependencyParameter is one parameter's reduced syntax for the
// FastAPI security-dependency linker (T026 AG-B4a): the annotation and the
// default as written, never evaluated. Calls inside carry their Span.
type PythonDependencyParameter struct {
	Name       string
	Annotation *RouteExpr `json:",omitempty"`
	Default    *RouteExpr `json:",omitempty"`
	Span       contract.Span
}

// PythonHelper retains a bounded signature rather than evaluating defaults.
type PythonHelper struct {
	Identity   string
	Async      bool
	Parameters []PythonParameter
}

// PythonParameter distinguishes Python's binding modes and unsafe rebinding.
type PythonParameter struct {
	Name, Kind       string
	Default, Blocked bool
}

// PythonHelperCall stores trimmed argument syntax for current-inventory binding.
type PythonHelperCall struct {
	Expression           RouteExpr
	Scope                int
	Conditional, Awaited bool
}

// PythonBinding records declarations, assignments and imported names without
// interpreting Python or caching a cross-file target.
type PythonBinding struct {
	Value      RouteExpr
	Identity   string
	Import     *Import
	Imported   string
	Count      int
	Stable     bool
	Bases      []RouteExpr
	ClassScope int
	// Guarded reports that some binding site of the name is conditional or
	// inside a try body (where an earlier failure skips it), so a
	// from-import of the name is undecided for the structural import rows.
	Guarded bool `json:",omitempty"`
}

// PythonRoute keeps decorator handlers separate from their enclosing owners.
type PythonRoute struct {
	Call       RouteCall
	Expression RouteExpr
	Handler    string
	Decorator  bool
	// InRoutesList marks a Starlette Mount(...) call written directly as
	// an element of a list literal passed as routes= to a call (T035,
	// `FastAPI(routes=[Mount("/x", app=sub)])`); RoutesCall is that
	// call's offset, the key the linker finds its object under.
	InRoutesList bool
	RoutesCall   int
}

// PythonModelsVersion identifies the model payload shape. Readers treat any
// other version as a cache miss, so a shape change never reuses stale syntax.
//
// Version 2 (ADR 0060) adds Queries and Annotations and widens the gate to
// G1 ∪ GQ; version 1 entries miss once.
const PythonModelsVersion = 2

// PythonModels is the model syntax that model linking (T018c2) and
// code-to-table linking (T018h) read, captured only in files that import
// sqlmodel or sqlalchemy (gate G1) or that call a session-named receiver's
// get, add, delete, merge or refresh (gate GQ). It is reduced syntax, never a
// claim: whether a base is SQLModel, a callee is Field, select is
// sqlalchemy's or a table exists is decided at link time through the
// resolver, under the claim contracts in docs/lang/python-models.md
// (ADR 0053) and docs/extract/table-access.md (ADR 0060). A file that passes
// GQ only has no Classes, CoreTables or Sites, so model linking reads
// exactly the files it read before.
type PythonModels struct {
	Version int
	Classes []PythonModelClass `json:",omitempty"`
	// CoreTables holds the 1-based lines of calls whose callee's last segment
	// is Table or map_imperatively (gate G4), so a view can say "tables
	// defined in code, not read" instead of "no tables".
	CoreTables []int `json:",omitempty"`
	// Sites holds post-definition mutation sites and DDL event hooks (gate
	// G5): they take completeness away from a model table, never add a fact.
	Sites []PythonModelSite `json:",omitempty"`
	// Queries holds the query-building and session-method call chains (ADR
	// 0060), in source order.
	Queries []PythonModelQuery `json:",omitempty"`
	// Annotations holds, per function scope, the annotations of the
	// parameters a query chain names, sorted by scope index.
	Annotations []PythonModelAnnotations `json:",omitempty"`
}

// PythonModelQuery is the outermost call of one call chain with a link in
// the closed list select, insert, update, delete, text, get, get_one, add,
// add_all, merge or refresh (each with its capture filter, ADR 0060). The
// statement or session call is kept whole so the linker proves the table
// where the statement is built, not where it runs.
type PythonModelQuery struct {
	// Span is the outermost call's span; its StartLine is the site's line.
	Span contract.Span
	// Scope is the PythonFacts.Scopes index of the enclosing scope, for
	// resolving names and reading local single assignments.
	Scope int `json:",omitempty"`
	// Owner is the identity of the innermost emitted function, method or
	// class around the call, a module-level constant for its initialiser,
	// else the file node: the source of the edge (T018h rule W1).
	Owner string
	// Expression is the reduced chain: the outermost call, whose Object
	// holds the call it is chained on, down to the first link.
	Expression PythonModelExpr
}

// PythonModelAnnotations is one function scope's annotated parameters that a
// query chain of the scope, or of a scope nested in it, names.
type PythonModelAnnotations struct {
	Scope int
	// Parameters maps a parameter name to its reduced annotation.
	Parameters map[string]PythonModelExpr
}

// PythonModelSite is one X1 mutation site or U5 DDL hook.
type PythonModelSite struct {
	// Kind is one of append_column, append_constraint, append_foreign_key,
	// set_add (indexes/constraints/foreign_keys .add), attribute_assign,
	// constraint_call or ddl_hook.
	Kind string
	Line int
	// Scope is the PythonFacts.Scopes index, for resolving local receivers.
	Scope int `json:",omitempty"`
	// Receiver is the receiver, assignment target or event target (reduced).
	Receiver PythonModelExpr
	// Args holds constraint_call arguments, the ddl_hook event name, or the
	// positional arguments of an append or set-add call reduced one level
	// (the constraint's callee decides what the site may change).
	Args []PythonModelExpr `json:",omitempty"`
	// Keywords holds the keyword arguments of an append or set-add call
	// (append_column's replace_existing may replace a defined column).
	Keywords []PythonModelKeyword `json:",omitempty"`
}

// PythonModelClass is a class header (gate G2) and, under gate G3, its body.
type PythonModelClass struct {
	Name string
	// Identity is the class node's identity, as the Python extractor gives it.
	Identity string `json:",omitempty"`
	// Scope is the PythonFacts.Scopes index of the enclosing scope.
	Scope         int `json:",omitempty"`
	Line, EndLine int
	// Conditional marks a definition inside if/try/for/while/with/match or a
	// function: such a class may not exist, so it is never claimed.
	Conditional     bool                 `json:",omitempty"`
	Bases           []PythonModelExpr    `json:",omitempty"`
	Keywords        []PythonModelKeyword `json:",omitempty"`
	Decorators      []PythonModelExpr    `json:",omitempty"`
	MembersCaptured bool
	Members         []PythonModelMember `json:",omitempty"`
}

// PythonModelMember is one class-body statement. Unread statements keep their
// kind and line so a view can show "partial" rather than an absence.
type PythonModelMember struct {
	// Kind is one of annotated, assign, method, class or other.
	Kind string
	// Name is empty for kind other.
	Name string `json:",omitempty"`
	Line int    `json:",omitempty"`
	// Annotation is set for annotated members only.
	Annotation *PythonModelExpr `json:",omitempty"`
	// Value is the assigned value of annotated (optional) and assign members.
	Value *PythonModelExpr `json:",omitempty"`
	// Decorators is set for methods only.
	Decorators []PythonModelExpr `json:",omitempty"`
	// Members is set for a nested class Config only (row M9).
	Members []PythonModelMember `json:",omitempty"`
	// Detail is the statement kind of an other member (if_statement, ...).
	Detail string `json:",omitempty"`
}

// PythonModelExpr is a reduced expression: literals decoded, calls with their
// dotted callee and reduced arguments, everything else kind other.
type PythonModelExpr struct {
	// Kind is one of name, literal, call, subscript, union, tuple, list,
	// dict, lambda or other.
	Kind string
	// Name is the dotted name (name), the dotted callee (call) or the
	// subscript base (subscript).
	Name string `json:",omitempty"`
	// Literal is the decoded value of a literal.
	Literal string `json:",omitempty"`
	// LitKind is one of str, int, float, bool, none, ellipsis or bytes.
	LitKind  string               `json:",omitempty"`
	Args     []PythonModelExpr    `json:",omitempty"`
	Keywords []PythonModelKeyword `json:",omitempty"`
	// Keys holds dict keys; Args holds the values.
	Keys []PythonModelExpr `json:",omitempty"`
	// Text is the compacted source of annotations and type expressions only.
	Text string `json:",omitempty"`
	// Object is set on a query chain link (PythonModelQuery) only: the
	// reduced expression the method Name is called on when it is not a
	// dotted name (select(M).join(N) is call join with Object call select).
	Object *PythonModelExpr `json:",omitempty"`
}

// PythonModelKeyword is a call or class keyword; Name is empty for **splat.
type PythonModelKeyword struct {
	Name  string `json:",omitempty"`
	Value PythonModelExpr
}

var (
	pythonModelMemberKinds = map[string]bool{"annotated": true, "assign": true, "method": true, "class": true, "other": true}
	pythonModelExprKinds   = map[string]bool{"name": true, "literal": true, "call": true, "subscript": true, "union": true, "tuple": true, "list": true, "dict": true, "lambda": true, "other": true}
	pythonModelLitKinds    = map[string]bool{"str": true, "int": true, "float": true, "bool": true, "none": true, "ellipsis": true, "bytes": true}
	pythonModelSiteKinds   = map[string]bool{"append_column": true, "append_constraint": true, "append_foreign_key": true, "set_add": true, "attribute_assign": true, "constraint_call": true, "ddl_hook": true}
)

// Valid accepts an absent payload (a file outside gate G1) or a structurally
// complete payload of the current version. Older or damaged payloads are a
// cache miss, so the file is re-extracted instead of trusted.
func (m *PythonModels) Valid() bool {
	if m == nil {
		return true
	}
	if m.Version != PythonModelsVersion {
		return false
	}
	for _, c := range m.Classes {
		if c.Name == "" || c.Line <= 0 || c.EndLine < c.Line || c.Scope < 0 || !c.MembersCaptured && len(c.Members) > 0 {
			return false
		}
		if !validModelExprs(c.Bases) || !validModelExprs(c.Decorators) || !validModelKeywords(c.Keywords) || !validModelMembers(c.Members) {
			return false
		}
	}
	for _, line := range m.CoreTables {
		if line <= 0 {
			return false
		}
	}
	for _, s := range m.Sites {
		if !pythonModelSiteKinds[s.Kind] || s.Line <= 0 || s.Scope < 0 || !s.Receiver.valid() || !validModelExprs(s.Args) || !validModelKeywords(s.Keywords) {
			return false
		}
	}
	for _, q := range m.Queries {
		if q.Span.StartLine <= 0 || q.Span.EndLine < q.Span.StartLine || q.Scope < 0 || q.Owner == "" || q.Expression.Kind != "call" || !q.Expression.valid() {
			return false
		}
	}
	for i, a := range m.Annotations {
		if a.Scope < 0 || len(a.Parameters) == 0 || i > 0 && m.Annotations[i-1].Scope >= a.Scope {
			return false
		}
		for name, e := range a.Parameters {
			if name == "" || !e.valid() {
				return false
			}
		}
	}
	return true
}

// ModelsValid also checks that every scope index of the model payload names
// a cached scope, because link-time resolution indexes Scopes with it.
func (p *PythonFacts) ModelsValid() bool {
	if p == nil || p.Models == nil {
		return true
	}
	if !p.Models.Valid() {
		return false
	}
	for _, c := range p.Models.Classes {
		if c.Scope >= len(p.Scopes) {
			return false
		}
	}
	for _, s := range p.Models.Sites {
		if s.Scope >= len(p.Scopes) {
			return false
		}
	}
	for _, q := range p.Models.Queries {
		if q.Scope >= len(p.Scopes) {
			return false
		}
	}
	for _, a := range p.Models.Annotations {
		if a.Scope >= len(p.Scopes) {
			return false
		}
	}
	return true
}

func validModelMembers(members []PythonModelMember) bool {
	for _, m := range members {
		if !pythonModelMemberKinds[m.Kind] || m.Kind != "other" && m.Name == "" || m.Kind != "class" && len(m.Members) > 0 {
			return false
		}
		if m.Annotation != nil && (m.Kind != "annotated" || !m.Annotation.valid()) {
			return false
		}
		if m.Value != nil && (m.Kind != "annotated" && m.Kind != "assign" || !m.Value.valid()) {
			return false
		}
		if len(m.Decorators) > 0 && m.Kind != "method" || !validModelExprs(m.Decorators) || !validModelMembers(m.Members) {
			return false
		}
	}
	return true
}

func validModelExprs(exprs []PythonModelExpr) bool {
	for _, e := range exprs {
		if !e.valid() {
			return false
		}
	}
	return true
}

func validModelKeywords(keywords []PythonModelKeyword) bool {
	for _, k := range keywords {
		if !k.Value.valid() {
			return false
		}
	}
	return true
}

func (e PythonModelExpr) valid() bool {
	if !pythonModelExprKinds[e.Kind] || e.LitKind != "" && (e.Kind != "literal" || !pythonModelLitKinds[e.LitKind]) || e.Kind == "literal" && e.LitKind == "" {
		return false
	}
	if e.Object != nil && (e.Kind != "call" || !e.Object.valid()) {
		return false
	}
	return validModelExprs(e.Args) && validModelExprs(e.Keys) && validModelKeywords(e.Keywords)
}

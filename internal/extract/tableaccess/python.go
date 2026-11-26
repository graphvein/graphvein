package tableaccess

import (
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// This file holds rows CT-P1..CT-P5 (SQLModel / SQLAlchemy) over the cached
// Python query payload (docs/lang/python-models.md, "Query sites") and the
// SQLModel group's table records (T018c2, ADR 0054).

// modelTable is one SQLModel group table: its node identity and the name
// and schema T018c2 gives it.
type modelTable struct {
	identity, name, schema string
	schemaKnown            bool
}

// sqlmodelTables reads the SQLModel group's tables from the model records,
// keyed by the class identity they carry.
func sqlmodelTables(nodes []contract.Node) map[string]modelTable {
	out := map[string]modelTable{}
	for _, n := range nodes {
		if n.Kind != "table" || n.Attributes["tool"] != "sqlmodel" {
			continue
		}
		class, _ := n.Attributes["class"].(string)
		if class == "" {
			continue
		}
		t := modelTable{identity: n.Identity}
		t.name, _ = n.Attributes["name"].(string)
		t.schema, _ = n.Attributes["schema"].(string)
		t.schemaKnown, _ = n.Attributes["schema_known"].(bool)
		out[class] = t
	}
	return out
}

// Statement functions by canonical name (module "." name) and the row
// they belong to. The pins are those of the SQLModel group (sqlmodel
// 0.0.39, sqlalchemy 2.0.51).
var pythonStatements = map[string]string{
	"sqlmodel.select": "select", "sqlalchemy.select": "select", "sqlalchemy.future.select": "select",
	"sqlmodel.sql.expression.select": "select", "sqlalchemy.sql.expression.select": "select",
	"sqlmodel.insert": "insert", "sqlalchemy.insert": "insert", "sqlmodel.update": "update", "sqlalchemy.update": "update",
	"sqlmodel.delete": "delete", "sqlalchemy.delete": "delete",
	"sqlalchemy.dialects.postgresql.insert": "insert", "sqlalchemy.dialects.sqlite.insert": "insert",
	"sqlalchemy.text": "text", "sqlmodel.text": "text",
}

// sessionClasses are the annotations that prove a CT-P3/CT-P4 receiver.
var sessionClasses = map[string]bool{
	"sqlmodel.Session": true, "sqlalchemy.orm.Session": true,
	"sqlalchemy.ext.asyncio.AsyncSession": true, "sqlmodel.ext.asyncio.session.AsyncSession": true,
}

// pythonExecutions are the session and connection methods that run a
// statement (Session.exec of SQLModel; execute, scalar, scalars, stream and
// stream_scalars of SQLAlchemy).
var pythonExecutions = map[string]bool{"exec": true, "execute": true, "scalar": true, "scalars": true, "stream": true, "stream_scalars": true}

// sessionOps maps a CT-P3/CT-P4 method to its row, direction and operation.
var sessionOps = map[string]struct {
	row, op string
	write   bool
}{
	"get": {"CT-P3", "get", false}, "get_one": {"CT-P3", "get", false},
	"add": {"CT-P4", "add", true}, "add_all": {"CT-P4", "add", true}, "delete": {"CT-P4", "delete", true},
	"merge": {"CT-P4", "merge", true}, "refresh": {"CT-P4", "refresh", false},
}

// ddlKeywords are keyword arguments whose text() value is a DDL fragment
// (a column default, a partial index predicate), never a query: such a
// text() call is no site (H-B1 follow-up).
var ddlKeywords = map[string]bool{"server_default": true, "server_onupdate": true, "postgresql_where": true, "sqlite_where": true, "default": true, "onupdate": true}

// pythonFile is one Python file's query sites in link order.
type pythonFile struct {
	in     Input
	file   string
	facts  facts.Facts
	models map[string]modelTable
	group  *migrationSet
	// executedLocal maps a query span to the execution calls that run it
	// through a local (R5-A1: statement = select(M); session.exec(statement)).
	executedLocal map[contract.Span][]contract.Span
}

// linkPythonFile reads a file's query sites and returns the spans of the
// recognised statement sites and of the executions that run one through a
// local (for the execution rule).
func linkPythonFile(in Input, file string, f facts.Facts, models map[string]modelTable, group *migrationSet, a *analysis) []contract.Span {
	l := pythonFile{in: in, file: file, facts: f, models: models, group: group}
	l.executedLocal = l.localExecutions()
	var spans []contract.Span
	for _, q := range f.Python.Models.Queries {
		s, statement, ok := l.site(q)
		if !ok {
			continue
		}
		if statement {
			spans = append(spans, q.Span)
			spans = append(spans, l.executedLocal[q.Span]...)
		}
		a.sites = append(a.sites, s)
	}
	return spans
}

// localExecutions applies R5-A1: an execution call (exec, execute, ...)
// whose only argument is a bare name, where that name has one stable
// assignment in a query's own scope whose value is that query's chain,
// with the same owner and after the assignment, runs that query. Any other
// shape (a conditional or repeated assignment, a name of an outer scope, a
// second argument) is not traced.
func (l pythonFile) localExecutions() map[contract.Span][]contract.Span {
	src := l.in.Sources[l.file]
	p := l.facts.Python
	out := map[contract.Span][]contract.Span{}
	for _, c := range l.facts.Calls {
		if !pythonExecutions[c.StaticName] || c.Source == "" {
			continue
		}
		name := soleNameArgument(src, c.Span)
		if name == "" {
			continue
		}
		for _, q := range p.Models.Queries {
			if q.Owner != c.Source || q.Scope < 0 || q.Scope >= len(p.Scopes) || !before(q.Span, c.Span) {
				continue
			}
			b, ok := p.Scopes[q.Scope].Bindings[name]
			if !ok || b.Count != 1 || !b.Stable || b.Import != nil || b.Value.Kind != "call" || b.Value.Object == nil {
				continue
			}
			if k := b.Value.Object.Kind; k != "identifier" && k != "attribute" {
				continue
			}
			if b.Value.Offset != offsetOf(src, q.Span.StartLine, q.Span.StartColumn) {
				continue
			}
			out[q.Span] = append(out[q.Span], c.Span)
		}
	}
	return out
}

// before reports whether span a ends before span b starts.
func before(a, b contract.Span) bool {
	return a.EndLine < b.StartLine || a.EndLine == b.StartLine && a.EndColumn <= b.StartColumn
}

// soleNameArgument returns the argument of a call whose argument list is
// one bare identifier (exec(statement)), read from the source; "" for any
// other call.
func soleNameArgument(src []byte, span contract.Span) string {
	start := offsetOf(src, span.StartLine, span.StartColumn)
	end := offsetOf(src, span.EndLine, span.EndColumn)
	if start < 0 || end <= start || end > len(src) || src[end-1] != ')' {
		return ""
	}
	open := strings.LastIndexByte(string(src[start:end-1]), '(')
	if open < 0 {
		return ""
	}
	arg := strings.TrimSpace(string(src[start+open+1 : end-1]))
	arg = strings.TrimSpace(strings.TrimSuffix(arg, ","))
	if arg == "" {
		return ""
	}
	for i, r := range arg {
		if r != '_' && !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(i > 0 && r >= '0' && r <= '9') {
			return ""
		}
	}
	return arg
}

// chain flattens a reduced chain into its links, first link first.
func chain(e facts.PythonModelExpr) []facts.PythonModelExpr {
	var links []facts.PythonModelExpr
	for cur := &e; cur != nil && cur.Kind == "call"; cur = cur.Object {
		links = append(links, *cur)
	}
	for i, j := 0, len(links)-1; i < j; i, j = i+1, j-1 {
		links[i], links[j] = links[j], links[i]
	}
	return links
}

// canonical resolves a dotted name to "module.name" when it names an
// external (library) binding, else "".
func (l pythonFile) canonical(scope int, dotted string) string {
	if dotted == "" || l.in.Resolver == nil {
		return ""
	}
	b := l.in.Resolver.Callable(l.file, scope, dotted)
	if !b.External || b.Name == "" {
		return ""
	}
	return b.File + "." + b.Name
}

// modelOf resolves a dotted name to the SQLModel group table of the model
// class it names (one stable class binding whose class node is a table of
// the group), ok false otherwise.
func (l pythonFile) modelOf(scope int, dotted string) (modelTable, bool) {
	if dotted == "" || l.in.Resolver == nil {
		return modelTable{}, false
	}
	b := l.in.Resolver.Callable(l.file, scope, dotted)
	if b.External || b.File == "" || b.Syntax.Value.Kind != "class_definition" || b.Syntax.Count != 1 || !b.Syntax.Stable {
		return modelTable{}, false
	}
	t, ok := l.models[b.Identity]
	return t, ok
}

// entity resolves a CT-P1 entity or join target: a model class, a model
// column M.c, col(M.c) or aliased(M); the form it matched is returned for
// the tag.
func (l pythonFile) entity(scope int, e facts.PythonModelExpr) (modelTable, string, bool) {
	switch e.Kind {
	case "name":
		if t, ok := l.modelOf(scope, e.Name); ok {
			return t, "model class", true
		}
		if i := strings.LastIndex(e.Name, "."); i > 0 {
			if t, ok := l.modelOf(scope, e.Name[:i]); ok {
				return t, "M.c", true
			}
		}
	case "call":
		if len(e.Args) != 1 || e.Object != nil {
			return modelTable{}, "", false
		}
		switch l.canonical(scope, e.Name) {
		case "sqlmodel.col":
			if t, form, ok := l.entity(scope, e.Args[0]); ok && form == "M.c" {
				return t, "col(M.c)", true
			}
		case "sqlalchemy.orm.aliased", "sqlalchemy.orm.util.aliased":
			if e.Args[0].Kind == "name" {
				if t, ok := l.modelOf(scope, e.Args[0].Name); ok {
					return t, "aliased(M)", true
				}
			}
		}
	}
	return modelTable{}, "", false
}

// site classifies one captured chain: a statement (CT-P1, CT-P2, CT-P5), a
// session method (CT-P3, CT-P4), or no site.
func (l pythonFile) site(q facts.PythonModelQuery) (site, bool, bool) {
	links := chain(q.Expression)
	if len(links) == 0 {
		return site{}, false, false
	}
	first := links[0]
	if first.Object == nil {
		if kind := pythonStatements[l.canonical(q.Scope, first.Name)]; kind != "" {
			switch kind {
			case "select":
				return l.claimPythonSelect(q, links), true, true
			case "text":
				if l.ddlFragment(q) {
					return site{}, false, false
				}
				return l.claimPythonText(q, first), true, true
			default:
				return l.claimPythonWrite(q, links, kind), true, true
			}
		}
	}
	// A session method is the outermost link (session.get(M) is one link;
	// a chained receiver keeps its earlier links in Object).
	last := q.Expression
	method := last.Name
	receiver := ""
	if last.Object == nil {
		i := strings.LastIndex(method, ".")
		if i <= 0 {
			return site{}, false, false
		}
		receiver, method = method[:i], method[i+1:]
	}
	op, ok := sessionOps[method]
	if !ok || len(last.Args) == 0 {
		return site{}, false, false
	}
	fact, named := l.sessionReceiver(q.Scope, receiver)
	if !fact && !named {
		// TA-V3c needs a receiver whose name says session: anything else
		// (a dict, a set, a router) is not recognised.
		return site{}, false, false
	}
	if op.row == "CT-P3" {
		s, ok := l.claimPythonGet(q, last, fact)
		return s, false, ok
	}
	return l.claimPythonObject(q, last, method, op.op, op.write, fact), false, true
}

// ddlFragment reports a text() call that is the value of a DDL keyword
// argument (server_default=, postgresql_where=, ...), read from the source
// just before the call.
func (l pythonFile) ddlFragment(q facts.PythonModelQuery) bool {
	src := l.in.Sources[l.file]
	off := offsetOf(src, q.Span.StartLine, q.Span.StartColumn)
	if off < 0 {
		return false
	}
	i := off - 1
	for i >= 0 && (src[i] == ' ' || src[i] == '\t' || src[i] == '\n' || src[i] == '\r') {
		i--
	}
	if i < 0 || src[i] != '=' || i > 0 && strings.ContainsRune("=!<>", rune(src[i-1])) {
		return false
	}
	i--
	for i >= 0 && (src[i] == ' ' || src[i] == '\t') {
		i--
	}
	end := i + 1
	for i >= 0 && (src[i] == '_' || src[i] >= 'a' && src[i] <= 'z' || src[i] >= 'A' && src[i] <= 'Z' || src[i] >= '0' && src[i] <= '9') {
		i--
	}
	return ddlKeywords[string(src[i+1:end])]
}

// offsetOf converts a 1-based line and 0-based byte column to an offset.
func offsetOf(src []byte, line, column int) int {
	cur := 1
	for i := 0; i < len(src); i++ {
		if cur == line {
			if i+column <= len(src) {
				return i + column
			}
			return -1
		}
		if src[i] == '\n' {
			cur++
		}
	}
	return -1
}

// sessionReceiver reports whether a receiver is proven (a parameter of the
// site's function scopes annotated with a session class) or named (a
// dotted name with a segment containing "session", or a parameter whose
// annotation's name contains it).
func (l pythonFile) sessionReceiver(scope int, receiver string) (fact, named bool) {
	if receiver == "" {
		return false, false
	}
	for _, part := range strings.Split(receiver, ".") {
		named = named || strings.Contains(strings.ToLower(part), "session")
	}
	if strings.Contains(receiver, ".") {
		return false, named
	}
	ann, at, ok := l.parameterAnnotation(scope, receiver)
	if !ok {
		return false, named
	}
	name := ann.Name
	if ann.Kind != "name" {
		return false, named
	}
	named = named || strings.Contains(strings.ToLower(name), "session")
	parent := l.facts.Python.Scopes[at].Parent
	if parent < 0 {
		parent = 0
	}
	return sessionClasses[l.canonical(parent, name)], named
}

// parameterAnnotation finds the annotation of the parameter a name refers
// to from scope: the nearest scope binding the name decides (a local
// assignment shadows an outer parameter). It returns the annotation and the
// parameter's function scope.
func (l pythonFile) parameterAnnotation(scope int, name string) (facts.PythonModelExpr, int, bool) {
	p := l.facts.Python
	for s := scope; s >= 0 && s < len(p.Scopes); {
		if hasString(p.Scopes[s].Parameters, name) {
			for _, a := range p.Models.Annotations {
				if a.Scope == s {
					e, ok := a.Parameters[name]
					return e, s, ok
				}
			}
			return facts.PythonModelExpr{}, s, false
		}
		if _, bound := p.Scopes[s].Bindings[name]; bound || s == 0 {
			return facts.PythonModelExpr{}, s, false
		}
		s = p.Scopes[s].Parent
	}
	return facts.PythonModelExpr{}, 0, false
}

func hasString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (l pythonFile) newSite(q facts.PythonModelQuery, row, tool string) site {
	return site{file: l.file, span: q.Span, owner: q.Owner, row: row, tool: tool, rows: []string{row},
		role: l.role(q), verification: rowByID(row).Verification,
		reads: map[string]map[string]bool{}, writes: map[string]map[string]bool{}}
}

// role is executes when an execution call of the same owner holds the site,
// runs it through a local (R5-A1), or the site is itself a session method,
// else builds.
func (l pythonFile) role(q facts.PythonModelQuery) string {
	if _, ok := sessionOps[lastSegment(q.Expression.Name)]; ok && q.Expression.Object == nil && strings.Contains(q.Expression.Name, ".") {
		return RoleExecutes
	}
	for _, c := range l.facts.Calls {
		if pythonExecutions[c.StaticName] && c.Source == q.Owner && contains(c.Span, q.Span) && c.Span != q.Span {
			return RoleExecutes
		}
	}
	if len(l.executedLocal[q.Span]) > 0 {
		return RoleExecutes
	}
	return RoleBuilds
}

func lastSegment(dotted string) string {
	return dotted[strings.LastIndex(dotted, ".")+1:]
}

func addOp(m map[string]map[string]bool, identity, op string) {
	if m[identity] == nil {
		m[identity] = map[string]bool{}
	}
	m[identity][op] = true
}

// claimPythonSelect is row CT-P1's branch: every entity and join target
// that resolves to a model of the group is read.
func (l pythonFile) claimPythonSelect(q facts.PythonModelQuery, links []facts.PythonModelExpr) site {
	s := l.newSite(q, "CT-P1", "sqlmodel")
	s.fact = true
	l.in.tag("CT-P1")
	unresolved, unresolvedCalls := 0, 0
	from := false
	// implicit are the models a call entity's arguments name
	// (func.max(Item.id)): SQLAlchemy adds them to the FROM list, a form the
	// row does not claim (Revision 3: site_partial).
	implicit := map[string]bool{}
	implicitOpen := false
	try := func(e facts.PythonModelExpr, entity bool) {
		t, form, ok := l.entity(q.Scope, e)
		if !ok {
			if entity && e.Kind == "call" {
				if refs, readable := l.modelRefs(q.Scope, e, 0); len(refs) > 0 {
					for _, id := range refs {
						implicit[id] = true
					}
					implicitOpen = implicitOpen || !readable
					return
				}
				// func.count() and the like: no table of their own when
				// select_from names the FROM.
				unresolvedCalls++
				return
			}
			unresolved++
			return
		}
		l.in.tag("CT-P1/" + form)
		addOp(s.reads, t.identity, "select")
	}
	for _, arg := range links[0].Args {
		try(arg, true)
	}
	for _, link := range links[1:] {
		switch link.Name {
		case "select_from", "join", "outerjoin":
			l.in.tag("CT-P1/." + link.Name)
			if len(link.Args) > 0 {
				before := len(s.reads)
				try(link.Args[0], false)
				from = from || link.Name == "select_from" && len(s.reads) > before
			}
		case "join_from":
			l.in.tag("CT-P1/.join_from")
			for i := 0; i < 2 && i < len(link.Args); i++ {
				try(link.Args[i], false)
			}
		}
	}
	if !from {
		unresolved += unresolvedCalls
	}
	if unresolved > 0 || len(s.reads) == 0 {
		s.refuse(ReasonEntityUnresolved, len(s.reads) > 0, nil)
		return s
	}
	var extra []string
	for id := range implicit {
		if s.reads[id] == nil {
			extra = append(extra, id)
		}
	}
	if len(extra) > 0 || implicitOpen && len(implicit) > 0 {
		sort.Strings(extra)
		if implicitOpen {
			extra = nil
		}
		s.refuse(ReasonSitePartial+": a function entity names a model the row does not claim (implicit FROM)", true, extra)
	}
	return s
}

// modelRefs lists the model tables a call entity's arguments name (M or
// M.c, at any depth of the reduced call); readable is false when an
// argument is a form the payload does not keep or a name that is no
// model, so other tables may hide in it.
func (l pythonFile) modelRefs(scope int, e facts.PythonModelExpr, depth int) ([]string, bool) {
	var refs []string
	readable := depth < 8
	var exprs []facts.PythonModelExpr
	exprs = append(exprs, e.Args...)
	for _, k := range e.Keywords {
		exprs = append(exprs, k.Value)
	}
	for _, a := range exprs {
		switch a.Kind {
		case "literal":
		case "name":
			if t, _, ok := l.entity(scope, a); ok {
				refs = append(refs, t.identity)
			} else {
				readable = false
			}
		case "call":
			r, ok := l.modelRefs(scope, a, depth+1)
			refs = append(refs, r...)
			readable = readable && ok
		default:
			readable = false
		}
	}
	return refs, readable
}

// claimPythonWrite is row CT-P2's branch: the statement's target model is
// written.
func (l pythonFile) claimPythonWrite(q facts.PythonModelQuery, links []facts.PythonModelExpr, kind string) site {
	s := l.newSite(q, "CT-P2", "sqlmodel")
	s.fact = true
	l.in.tag("CT-P2/" + kind)
	op := kind
	for _, link := range links[1:] {
		if link.Name == "on_conflict_do_update" {
			l.in.tag("CT-P2/on_conflict_do_update")
			op = "upsert"
		}
	}
	var t modelTable
	ok := false
	if len(links[0].Args) > 0 && links[0].Args[0].Kind == "name" {
		t, ok = l.modelOf(q.Scope, links[0].Args[0].Name)
	}
	if !ok {
		s.refuse(ReasonEntityUnresolved, false, nil)
		return s
	}
	addOp(s.writes, t.identity, op)
	if extra := l.predicateModels(q.Scope, links[1:], t.identity); len(extra) > 0 {
		// update(User).where(User.id == Item.owner_id) can read item (an
		// UPDATE ... FROM): the row claims only the target (R5-C3).
		s.refuse(ReasonSitePartial+": a predicate names a model the row does not claim", true, extra)
	}
	return s
}

// predicateModels lists the model tables other than the target that a
// write's .where/.filter arguments name where the payload keeps them: a
// model class or column (M, M.c), a column method (M.c.is_(...)), or such
// a name inside a call's arguments. A nested statement (select(...)) is
// its own site and is not read here; a comparison or other operator is
// kind other in the payload, so a model named only there is not seen (the
// documented gap, as for .where in selects).
func (l pythonFile) predicateModels(scope int, links []facts.PythonModelExpr, target string) []string {
	found := map[string]bool{}
	var walk func(e facts.PythonModelExpr, depth int)
	walk = func(e facts.PythonModelExpr, depth int) {
		if depth > 8 {
			return
		}
		switch e.Kind {
		case "name":
			if t, _, ok := l.entity(scope, e); ok {
				found[t.identity] = true
			}
		case "call":
			if e.Object == nil && pythonStatements[l.canonical(scope, e.Name)] != "" {
				return
			}
			if e.Object == nil {
				if i := strings.LastIndex(e.Name, "."); i > 0 {
					walk(facts.PythonModelExpr{Kind: "name", Name: e.Name[:i]}, depth+1)
				}
			}
			for _, a := range e.Args {
				walk(a, depth+1)
			}
		case "list", "tuple":
			for _, a := range e.Args {
				walk(a, depth+1)
			}
		}
	}
	for _, link := range links {
		if link.Name != "where" && link.Name != "filter" {
			continue
		}
		for _, a := range link.Args {
			walk(a, 0)
		}
	}
	delete(found, target)
	return sortedSet(found)
}

// receiverProof sets a session site's provenance: fact when the receiver
// is proven, else inferred under TA-V3c.
func (s *site) receiverProof(fact bool) {
	s.fact = fact
	if !fact {
		s.assumes = []string{"TA-V3c"}
	}
}

// claimPythonGet is row CT-P3's branch: get/get_one of a model class reads
// it. A first argument that is no model is not a site.
func (l pythonFile) claimPythonGet(q facts.PythonModelQuery, call facts.PythonModelExpr, fact bool) (site, bool) {
	if call.Args[0].Kind != "name" {
		return site{}, false
	}
	t, ok := l.modelOf(q.Scope, call.Args[0].Name)
	if !ok {
		return site{}, false
	}
	l.in.tag("CT-P3/" + lastSegment(call.Name))
	s := l.newSite(q, "CT-P3", "sqlmodel")
	s.receiverProof(fact)
	addOp(s.reads, t.identity, "get")
	return s, true
}

// claimPythonObject is row CT-P4's branch: the proven class of each object
// is written (add, delete, merge) or read (refresh).
func (l pythonFile) claimPythonObject(q facts.PythonModelQuery, call facts.PythonModelExpr, method, op string, write bool, fact bool) site {
	l.in.tag("CT-P4/" + method)
	s := l.newSite(q, "CT-P4", "sqlmodel")
	s.receiverProof(fact)
	objects := []facts.PythonModelExpr{call.Args[0]}
	if method == "add_all" {
		if call.Args[0].Kind != "list" || len(call.Args[0].Args) == 0 {
			s.refuse(ReasonObjectClass, false, nil)
			return s
		}
		objects = call.Args[0].Args
	}
	var proven []modelTable
	for _, o := range objects {
		t, ok := l.objectClass(q.Scope, o)
		if !ok {
			// One unproven object leaves the site unproven: claim none.
			s.refuse(ReasonObjectClass, false, nil)
			return s
		}
		proven = append(proven, t)
	}
	for _, t := range proven {
		if write {
			addOp(s.writes, t.identity, op)
		} else {
			addOp(s.reads, t.identity, op)
		}
	}
	return s
}

// objectClass proves the class of a CT-P4 object: M(...), M.model_validate
// (...), M.from_orm(...), a local with one unconditional assignment from one
// of those in the site's own scope, or a parameter of that scope annotated
// M, M | None or Optional[M].
func (l pythonFile) objectClass(scope int, o facts.PythonModelExpr) (modelTable, bool) {
	switch o.Kind {
	case "call":
		if o.Object != nil {
			return modelTable{}, false
		}
		return l.constructor(scope, o.Name)
	case "name":
		if strings.Contains(o.Name, ".") {
			return modelTable{}, false
		}
		p := l.facts.Python
		if scope < 0 || scope >= len(p.Scopes) {
			return modelTable{}, false
		}
		if hasString(p.Scopes[scope].Parameters, o.Name) {
			ann, at, ok := l.parameterAnnotation(scope, o.Name)
			if !ok || at != scope {
				return modelTable{}, false
			}
			parent := p.Scopes[at].Parent
			if parent < 0 {
				parent = 0
			}
			return l.annotatedModel(parent, ann)
		}
		b, ok := p.Scopes[scope].Bindings[o.Name]
		if !ok || b.Count != 1 || !b.Stable || b.Import != nil || b.Value.Kind != "call" || b.Value.Object == nil {
			return modelTable{}, false
		}
		return l.constructor(scope, routeDotted(*b.Value.Object))
	}
	return modelTable{}, false
}

// constructor reads M, M.model_validate or M.from_orm as a callee.
func (l pythonFile) constructor(scope int, callee string) (modelTable, bool) {
	if callee == "" {
		return modelTable{}, false
	}
	if t, ok := l.modelOf(scope, callee); ok {
		l.in.tag("CT-P4/M(...)")
		return t, true
	}
	i := strings.LastIndex(callee, ".")
	if i <= 0 {
		return modelTable{}, false
	}
	method := callee[i+1:]
	if method != "model_validate" && method != "from_orm" {
		return modelTable{}, false
	}
	t, ok := l.modelOf(scope, callee[:i])
	if ok {
		l.in.tag("CT-P4/M." + method)
	}
	return t, ok
}

// annotatedModel reads M, M | None and Optional[M].
func (l pythonFile) annotatedModel(scope int, e facts.PythonModelExpr) (modelTable, bool) {
	switch e.Kind {
	case "name":
		return l.modelOf(scope, e.Name)
	case "union":
		var model *facts.PythonModelExpr
		for i := range e.Args {
			a := e.Args[i]
			if a.Kind == "literal" && a.LitKind == "none" {
				continue
			}
			if model != nil {
				return modelTable{}, false
			}
			model = &e.Args[i]
		}
		if model != nil && model.Kind == "name" && len(e.Args) == 2 {
			return l.modelOf(scope, model.Name)
		}
	case "subscript":
		if len(e.Args) == 1 && e.Args[0].Kind == "name" && l.canonical(scope, e.Name) == "typing.Optional" {
			return l.modelOf(scope, e.Args[0].Name)
		}
	}
	return modelTable{}, false
}

// routeDotted renders an identifier or attribute chain as a dotted name.
func routeDotted(e facts.RouteExpr) string {
	switch e.Kind {
	case "identifier":
		return e.Name
	case "attribute":
		if e.Object == nil {
			return ""
		}
		head := routeDotted(*e.Object)
		if head == "" {
			return ""
		}
		return head + "." + e.Name
	}
	return ""
}

// claimPythonText is row CT-P5's branch: a literal text() through the SQL
// reader against the SQLModel group's tables (O3), every edge inferred
// under TA-V1.
func (l pythonFile) claimPythonText(q facts.PythonModelQuery, call facts.PythonModelExpr) site {
	l.in.tag("CT-P5")
	s := l.newSite(q, "CT-P5", "sql")
	s.assumes = []string{"TA-V1"}
	s.reads, s.writes = nil, nil
	if len(call.Args) == 0 || call.Args[0].Kind != "literal" || call.Args[0].LitKind != "str" {
		s.refuse(ReasonNotLiteral, false, nil)
		return s
	}
	if l.alembicRevision() {
		// An Alembic revision runs against the schema at its revision, not
		// the models' end state: TA-V1 does not hold.
		s.refuse(ReasonNoSchemaSource+": an Alembic revision module runs against the schema at its revision", false, nil)
		return s
	}
	if l.group == nil || len(l.group.tables) == 0 {
		s.refuse(ReasonNoSchemaSource+": no SQLModel table in the repository", false, nil)
		return s
	}
	s.sql = call.Args[0].Literal
	s.sets = []*migrationSet{l.group}
	readSQLSite(l.in, &s, s.sql, s.sets)
	return s
}

// alembicRevision reports an Alembic revision module: module-level
// bindings of both revision and down_revision.
func (l pythonFile) alembicRevision() bool {
	scopes := l.facts.Python.Scopes
	if len(scopes) == 0 {
		return false
	}
	_, rev := scopes[0].Bindings["revision"]
	_, down := scopes[0].Bindings["down_revision"]
	return rev && down
}

// sqlmodelGroupName names the SQLModel group as a schema source.
const sqlmodelGroupName = "models/sqlmodel/."

// sqlmodelGroup is the SQLModel group as a schema source for CT-Q2: its
// tables by name and schema; a table whose schema T018c2 could not prove
// is unresolved, as an O3.P13 migration table is.
func sqlmodelGroup(models map[string]modelTable) *migrationSet {
	g := &migrationSet{name: sqlmodelGroupName, dialect: "postgres"}
	for _, t := range models {
		g.tables = append(g.tables, table{identity: t.identity, schema: t.schema, name: t.name, pathUnproven: !t.schemaKnown && t.schema == ""})
	}
	sort.Slice(g.tables, func(i, j int) bool { return g.tables[i].identity < g.tables[j].identity })
	return g
}

// unknownPythonExecution is row D's branch for Python executions: in a
// captured file, an exec/execute/scalar(s)/stream call whose span holds no
// recognised statement site runs a statement whose table is not traced
// here.
func unknownPythonExecution(in Input, f facts.Facts, sites []contract.Span, a *analysis) {
	for _, c := range f.Calls {
		if !pythonExecutions[c.StaticName] || c.Source == "" {
			continue
		}
		traced := false
		for _, s := range sites {
			traced = traced || contains(c.Span, s)
		}
		if traced {
			continue
		}
		in.tag("D")
		a.unknowns[c.Source] = append(a.unknowns[c.Source], Unknown{Row: "D", Line: c.Span.StartLine, Span: c.Span, Tool: "sqlmodel", Reason: ReasonStatementNotTraced})
	}
}

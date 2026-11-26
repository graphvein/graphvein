package python

import (
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// This file captures the query sites that code-to-table linking (T018h,
// ADR 0060) reads from Python: the outermost call of each call chain with a
// link in a closed list of names, reduced with its arguments, and the
// annotations of the parameters those chains name. It records syntax only;
// whether select is sqlalchemy's, a receiver is a session or an argument is
// a model class is decided at link time (docs/lang/python-models.md,
// "Query sites").

const (
	// queryDepth bounds the reduced arguments of a chain link: enough for
	// select(func.count(col(M.id))) and add_all([M(...), N(...)]).
	queryDepth = 4
)

var (
	// queryFunctions are the statement constructors (CT-P1, P2, P5): the
	// link's callee must be a dotted name, a bare name or a path whose root
	// an import of the file binds (sa.select).
	queryFunctions = setOf("select", "insert", "update", "delete", "text")
	// queryMethods are the session methods (CT-P3, P4): the link is a method
	// call whose first positional argument is not a literal. get and get_one
	// also need a dotted name there (the model class).
	queryMethods = setOf("get", "get_one", "add", "add_all", "delete", "merge", "refresh")
	// gateMethods are the methods of gate GQ: every session method.
	gateMethods = queryMethods
)

// queryCandidate is a captured chain before the file gate is known.
type queryCandidate struct {
	query facts.PythonModelQuery
	names map[string]bool // first segments of the dotted names the chain holds
	gq    bool            // a link passes gate GQ by its receiver's name
	// receivers are the first segments of the session-method receivers,
	// for gate GQ's annotation test in finish.
	receivers []string
}

// annotated is one annotated parameter of a function scope.
type annotated struct {
	scope int
	name  string
	node  *parse.Node
}

// queryCapture collects chains and parameter annotations during the syntax
// walk of every Python file; finish keeps them only for G1 ∪ GQ files.
type queryCapture struct {
	// imported maps a name an import binds to the imported name's last
	// segment (insert as pg_insert: pg_insert -> insert).
	imported   map[string]string
	links      map[*parse.Node]bool // calls that are a link of an outer chain
	candidates []queryCandidate
	params     []annotated
}

func newQueryCapture(imports []facts.Import) *queryCapture {
	q := &queryCapture{imported: map[string]string{}, links: map[*parse.Node]bool{}}
	for _, imp := range imports {
		for local, name := range imp.Bindings {
			if !strings.Contains(local, ".") {
				q.imported[local] = lastSegment(name)
			} else if first := strings.SplitN(local, ".", 2)[0]; q.imported[first] == "" {
				q.imported[first] = first
			}
		}
	}
	return q
}

// parameter records an annotated parameter of a function scope.
func (q *queryCapture) parameter(scope int, c *parse.Node) {
	if c.Kind != "typed_parameter" && c.Kind != "typed_default_parameter" {
		return
	}
	name, typ := c.Fields["name"], c.Fields["type"]
	if name == nil {
		for _, v := range c.Children {
			if v.Kind == "identifier" {
				name = v
				break
			}
		}
	}
	if name != nil && name.Kind == "identifier" && typ != nil {
		q.params = append(q.params, annotated{scope: scope, name: name.Text, node: typ})
	}
}

// chainLinks lists the calls of the chain whose outermost call is n, from
// the outermost inwards: select(M).join(N) gives join, select.
func chainLinks(n *parse.Node) []*parse.Node {
	var links []*parse.Node
	for n != nil && n.Kind == "call" {
		links = append(links, n)
		callee := unwrapModel(n.Fields["function"])
		if callee == nil || callee.Kind != "attribute" {
			break
		}
		n = unwrapModel(callee.Fields["object"])
	}
	return links
}

// call examines a call the walk reaches. A call that is a link of an outer
// chain was examined with that chain.
func (q *queryCapture) call(n *parse.Node, scope int, owner string) {
	if q.links[n] {
		return
	}
	links := chainLinks(n)
	for _, l := range links[1:] {
		q.links[l] = true
	}
	keep, gq := false, false
	var receivers []string
	for _, l := range links {
		k, g, r := q.link(l)
		keep, gq = keep || k, gq || g
		if r != "" {
			receivers = append(receivers, r)
		}
	}
	if !keep {
		return
	}
	expr := reduceQuery(n, 1)
	names := map[string]bool{}
	queryNames(expr, names)
	q.candidates = append(q.candidates, queryCandidate{query: facts.PythonModelQuery{Span: facts.Span(n), Scope: scope, Owner: owner, Expression: expr}, names: names, gq: gq, receivers: receivers})
}

// link applies the capture filter of one chain link and gate GQ's
// receiver-name test; it also returns the first segment of a passing session
// method's dotted receiver, for GQ's annotation test.
func (q *queryCapture) link(n *parse.Node) (keep, gq bool, receiver string) {
	callee := unwrapModel(n.Fields["function"])
	if callee == nil {
		return false, false, ""
	}
	dotted := dottedModel(callee)
	name := lastSegment(dotted)
	if callee.Kind == "attribute" {
		name = facts.Text(callee.Fields["attribute"])
	}
	if callee.Kind == "identifier" && q.imported[dotted] != "" {
		// An import alias names the imported function (insert as pg_insert).
		if queryFunctions[q.imported[dotted]] {
			keep = true
		}
	} else if queryFunctions[name] && dotted != "" && (!strings.Contains(dotted, ".") || q.imported[strings.SplitN(dotted, ".", 2)[0]] != "") {
		keep = true
	}
	if !queryMethods[name] || callee.Kind != "attribute" {
		return keep, false, ""
	}
	var first *parse.Node
	for _, a := range callArgs(n) {
		if a.Kind != "keyword_argument" && a.Kind != "dictionary_splat" {
			first = unwrapModel(a)
			break
		}
	}
	if first == nil || first.Kind == "list_splat" {
		return keep, false, ""
	}
	switch first.Kind {
	case "string", "concatenated_string", "integer", "float", "true", "false", "none":
		return keep, false, ""
	}
	if (name == "get" || name == "get_one") && dottedModel(first) == "" {
		return keep, false, ""
	}
	object := dottedModel(callee.Fields["object"])
	if object == "" {
		return true, false, ""
	}
	return true, gateMethods[name] && sessionNamed(lastSegment(object)), strings.SplitN(object, ".", 2)[0]
}

// sessionNamed is gate GQ's spelling test.
func sessionNamed(s string) bool {
	return strings.Contains(strings.ToLower(s), "session")
}

// reduceQuery reduces a chain link and its arguments. Links of one chain
// share a depth; arguments go one level deeper, up to queryDepth. Keyword
// values are not read by any row and keep their names only.
func reduceQuery(n *parse.Node, depth int) facts.PythonModelExpr {
	n = unwrapModel(n)
	if n == nil || depth > queryDepth {
		return facts.PythonModelExpr{Kind: "other"}
	}
	switch n.Kind {
	case "call":
		e := facts.PythonModelExpr{Kind: "call"}
		callee := unwrapModel(n.Fields["function"])
		if dotted := dottedModel(callee); dotted != "" {
			e.Name = dotted
		} else if callee != nil && callee.Kind == "attribute" {
			e.Name = facts.Text(callee.Fields["attribute"])
			object := reduceQuery(callee.Fields["object"], depth)
			e.Object = &object
		}
		for _, a := range callArgs(n) {
			switch a.Kind {
			case "keyword_argument":
				e.Keywords = append(e.Keywords, facts.PythonModelKeyword{Name: facts.Text(a.Fields["name"]), Value: facts.PythonModelExpr{Kind: "other"}})
			case "dictionary_splat":
				e.Keywords = append(e.Keywords, facts.PythonModelKeyword{Value: facts.PythonModelExpr{Kind: "other"}})
			case "list_splat":
				e.Args = append(e.Args, facts.PythonModelExpr{Kind: "other"})
			default:
				e.Args = append(e.Args, reduceQuery(a, depth+1))
			}
		}
		return e
	case "identifier", "attribute":
		if name := dottedModel(n); name != "" {
			return facts.PythonModelExpr{Kind: "name", Name: name}
		}
	case "list", "tuple":
		kind := "tuple"
		if n.Kind == "list" {
			kind = "list"
		}
		e := facts.PythonModelExpr{Kind: kind}
		for _, c := range n.Children {
			if c.Kind != "comment" {
				e.Args = append(e.Args, reduceQuery(c, depth+1))
			}
		}
		return e
	case "string", "concatenated_string", "integer", "float", "true", "false", "none":
		// Literals as the model payload decodes them; an f-string with an
		// interpolation, %-formatting or a .format call is kind other.
		return reduceModel(n, depth, depth)
	}
	return facts.PythonModelExpr{Kind: "other"}
}

// queryNames collects the first segment of every dotted name and callee in
// a reduced chain: the names a parameter annotation can prove.
func queryNames(e facts.PythonModelExpr, names map[string]bool) {
	if e.Name != "" && (e.Kind == "name" || e.Kind == "call" && e.Object == nil) {
		names[strings.SplitN(e.Name, ".", 2)[0]] = true
	}
	if e.Object != nil {
		queryNames(*e.Object, names)
	}
	for _, a := range e.Args {
		queryNames(a, names)
	}
}

// finish returns the chains and annotations of a file in gate G1 or GQ, or
// nil and false for any other file.
func (q *queryCapture) finish(g1 bool, scopes []facts.PythonScope) ([]facts.PythonModelQuery, []facts.PythonModelAnnotations, bool) {
	// Gate GQ: a session method whose receiver is session-named, or is a
	// parameter of an enclosing function annotated with a session-named type
	// (db: DbSession, db: AsyncSession).
	sessionParams := map[int]map[string]bool{}
	for _, p := range q.params {
		if sessionNamed(p.node.Text) {
			if sessionParams[p.scope] == nil {
				sessionParams[p.scope] = map[string]bool{}
			}
			sessionParams[p.scope][p.name] = true
		}
	}
	gq := false
	for _, c := range q.candidates {
		gq = gq || c.gq
		for _, r := range c.receivers {
			for s := c.query.Scope; !gq && s > 0 && s < len(scopes); s = scopes[s].Parent {
				gq = sessionParams[s][r]
			}
		}
	}
	if !g1 && !gq {
		return nil, nil, false
	}
	var queries []facts.PythonModelQuery
	wanted := map[int]map[string]bool{} // function scope -> parameter names named by a chain in it or below
	for _, c := range q.candidates {
		queries = append(queries, c.query)
		for s := c.query.Scope; s > 0 && s < len(scopes); s = scopes[s].Parent {
			if scopes[s].Class {
				continue
			}
			if wanted[s] == nil {
				wanted[s] = map[string]bool{}
			}
			for name := range c.names {
				wanted[s][name] = true
			}
		}
	}
	byScope := map[int]map[string]facts.PythonModelExpr{}
	for _, p := range q.params {
		if !wanted[p.scope][p.name] {
			continue
		}
		if byScope[p.scope] == nil {
			byScope[p.scope] = map[string]facts.PythonModelExpr{}
		}
		byScope[p.scope][p.name] = reduceModel(p.node, 1, modelAnnotationDepth)
	}
	var annotations []facts.PythonModelAnnotations
	for scope, params := range byScope {
		annotations = append(annotations, facts.PythonModelAnnotations{Scope: scope, Parameters: params})
	}
	sort.Slice(annotations, func(i, j int) bool { return annotations[i].Scope < annotations[j].Scope })
	return queries, annotations, true
}

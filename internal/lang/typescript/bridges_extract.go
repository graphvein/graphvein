package typescript

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// This file collects the syntax of the bridge rows (T026,
// docs/lang/typescript-bridges.md): JSX element tags, function values passed
// to calls and JSX attributes, module-level React.lazy bindings and
// react-router route candidates. Nothing here is resolved; bridges.go links
// the cached sites against the current inventory.

// routerFactories are the react-router functions whose first argument is a
// route array (react-router 7.13.0 and 7.18.2, also re-exported by
// react-router-dom 7; createBrowserRouter, createHashRouter and
// createMemoryRouter exist since 6.4).
var routerFactories = map[string]bool{"createBrowserRouter": true, "createHashRouter": true, "createMemoryRouter": true, "useRoutes": true}

// routeKeys are the keys that make a module-level const object a route
// candidate (a const that no proven router reaches emits nothing).
var routeKeys = map[string]bool{"path": true, "index": true, "element": true, "Component": true, "loader": true, "action": true, "lazy": true, "children": true}

// bridgeCollector gathers one file's bridge syntax during Extract's walk.
type bridgeCollector struct {
	file   string
	source []byte
	syn    facts.BridgeSyntax
	// scopes are the local names of each named function scope (parameters
	// with their binding patterns and every non-function declaration of
	// the body), collected before the body is walked: a local binding
	// shadows throughout its scope.
	scopes map[string]map[string]bool
	// frames are the local names of the enclosing anonymous functions,
	// innermost last.
	frames []map[string]bool
	// routes is the stack of enclosing <Route> candidates.
	routes []int
	// objects maps an object literal to its route candidate.
	objects map[*parse.Node]int
	starts  []uint32
}

func newBridgeCollector(file string, source []byte) *bridgeCollector {
	return &bridgeCollector{file: file, source: source, scopes: map[string]map[string]bool{}, objects: map[*parse.Node]int{}}
}

// shadowed reports whether base names a local binding at the site.
func (bc *bridgeCollector) shadowed(scope, base string) bool {
	for _, frame := range bc.frames {
		if frame[base] {
			return true
		}
	}
	for {
		if bc.scopes[scope][base] {
			return true
		}
		if scope == "" {
			return false
		}
		if i := strings.LastIndex(scope, "."); i >= 0 {
			scope = scope[:i]
		} else {
			scope = ""
		}
	}
}

// site builds a BridgeSite for a name read at n.
func (bc *bridgeCollector) site(row, caller, scope, name string, n *parse.Node) facts.BridgeSite {
	name = strings.ReplaceAll(name, "?.", ".")
	base, _, _ := strings.Cut(name, ".")
	return facts.BridgeSite{Row: row, Source: caller, Scope: scope, Name: name, Span: span(n), Shadowed: bc.shadowed(scope, base)}
}

// enterFunction records a named function's locals before its body is
// walked.
func (bc *bridgeCollector) enterFunction(q string, n *parse.Node) {
	bc.scopes[q] = functionLocals(n)
}

// pushFrame and popFrame bracket the walk of an anonymous function.
func (bc *bridgeCollector) pushFrame(n *parse.Node) { bc.frames = append(bc.frames, functionLocals(n)) }
func (bc *bridgeCollector) popFrame()               { bc.frames = bc.frames[:len(bc.frames)-1] }

// functionLocals are a function's parameter names and the names its body
// declares outside nested functions (function-valued declarations are
// declarations of their own and stay resolvable).
func functionLocals(fn *parse.Node) map[string]bool {
	names := map[string]bool{}
	params := fn.Fields["parameters"]
	if params == nil {
		params = fn.Fields["parameter"]
	}
	bindingPatternNames(params, names)
	var walk func(*parse.Node)
	walk = func(n *parse.Node) {
		if n == nil {
			return
		}
		switch n.Kind {
		case "function_declaration", "generator_function_declaration", "function_expression", "generator_function", "arrow_function", "method_definition", "class_declaration", "class", "abstract_class_declaration":
			return
		case "variable_declarator":
			if v := n.Fields["value"]; v == nil || !(v.Kind == "arrow_function" || v.Kind == "function_expression" || v.Kind == "generator_function") || n.Fields["name"] == nil || n.Fields["name"].Kind != "identifier" {
				bindingPatternNames(n.Fields["name"], names)
			}
		case "catch_clause":
			bindingPatternNames(n.Fields["parameter"], names)
		case "for_in_statement":
			bindingPatternNames(n.Fields["left"], names)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(fn.Fields["body"])
	return names
}

// bindingPatternNames adds the identifiers a binding pattern or parameter list
// binds (not default values or type annotations).
func bindingPatternNames(n *parse.Node, names map[string]bool) {
	if n == nil {
		return
	}
	switch n.Kind {
	case "identifier", "shorthand_property_identifier_pattern":
		names[n.Text] = true
		return
	case "type_annotation", "this":
		return
	case "pair_pattern":
		bindingPatternNames(n.Fields["value"], names)
		return
	case "assignment_pattern", "object_assignment_pattern":
		bindingPatternNames(n.Fields["left"], names)
		return
	case "required_parameter", "optional_parameter":
		bindingPatternNames(n.Fields["pattern"], names)
		return
	}
	for _, c := range n.Children {
		bindingPatternNames(c, names)
	}
}

// intrinsicTag reports a JSX tag the compiler treats as an intrinsic
// element (isIntrinsicJsxName: a lower-case first letter or a dash).
func intrinsicTag(name string) bool {
	if strings.Contains(name, "-") {
		return true
	}
	for _, r := range name {
		return unicode.IsLower(r)
	}
	return true
}

// tagName returns a JSX tag's name as written when it is an identifier or
// a member chain of identifiers (this included); "" for a fragment, a
// namespace name or any other form.
func tagName(n *parse.Node) string {
	name := n.Fields["name"]
	if name == nil {
		return ""
	}
	switch name.Kind {
	case "identifier":
		return name.Text
	case "member_expression", "nested_identifier":
		if memberChain(name) {
			return name.Text
		}
	}
	return ""
}

// memberChain reports a.b.c of identifiers (this allowed at the root).
func memberChain(n *parse.Node) bool {
	switch n.Kind {
	case "identifier", "this":
		return true
	case "member_expression", "nested_identifier":
		object, property := n.Fields["object"], n.Fields["property"]
		if object == nil && len(n.Children) == 2 {
			object, property = n.Children[0], n.Children[1]
		}
		return object != nil && property != nil && (property.Kind == "property_identifier" || property.Kind == "identifier") && memberChain(object) && !strings.Contains(n.Text, "?.") && !strings.ContainsAny(n.Text, " \t\n/")
	}
	return false
}

// valueName returns a passed value's name when it is an identifier or a
// member chain (this.x included), else "".
func valueName(n *parse.Node) string {
	if n == nil {
		return ""
	}
	if n.Kind == "identifier" || n.Kind == "member_expression" && memberChain(n) {
		return n.Text
	}
	return ""
}

// tag records the tag of a JSX opening or self-closing element (row
// JR, sites of Row "jsx"). Intrinsic elements and fragments are D.
func (bc *bridgeCollector) tag(n *parse.Node, owner, caller string) {
	name := tagName(n)
	if name == "" || !strings.Contains(name, ".") && intrinsicTag(name) {
		bridgeTag("D")
		return
	}
	bc.syn.Sites = append(bc.syn.Sites, bc.site("jsx", caller, owner, name, n))
}

// attribute records a JSX attribute whose value is a name (row PF3).
func (bc *bridgeCollector) attribute(n *parse.Node, owner, caller string) {
	if len(n.Children) != 2 || n.Children[1].Kind != "jsx_expression" || len(n.Children[1].Children) != 1 {
		return
	}
	if name := valueName(n.Children[1].Children[0]); name != "" {
		bc.syn.Sites = append(bc.syn.Sites, bc.site("attribute", caller, owner, name, n.Children[1].Children[0]))
	}
}

// call records the names passed as arguments (row PF1), the names in
// object and array literals of an argument (row PF2) and router factory
// calls.
func (bc *bridgeCollector) call(n *parse.Node, owner, caller string) {
	callee := n.Fields["function"]
	args := n.Fields["arguments"]
	if callee == nil || args == nil || args.Kind != "arguments" || callee.Kind == "import" || callee.Kind == "arrow_function" {
		return
	}
	var literal func(*parse.Node)
	literal = func(v *parse.Node) {
		for _, c := range v.Children {
			switch c.Kind {
			case "pair":
				value := c.Fields["value"]
				if name := valueName(value); name != "" {
					bc.syn.Sites = append(bc.syn.Sites, bc.site("property", caller, owner, name, value))
				} else if value != nil && (value.Kind == "object" || value.Kind == "array") {
					literal(value)
				}
			case "shorthand_property_identifier":
				bc.syn.Sites = append(bc.syn.Sites, bc.site("property", caller, owner, c.Text, c))
			case "object", "array":
				literal(c)
			default:
				if v.Kind == "array" {
					if name := valueName(c); name != "" {
						bc.syn.Sites = append(bc.syn.Sites, bc.site("property", caller, owner, name, c))
					}
				}
			}
		}
	}
	for _, a := range args.Children {
		if name := valueName(a); name != "" {
			bc.syn.Sites = append(bc.syn.Sites, bc.site("argument", caller, owner, name, a))
		} else if a.Kind == "object" || a.Kind == "array" {
			literal(a)
		}
	}
	factory := ""
	switch {
	case callee.Kind == "identifier":
		factory = callee.Text
	case callee.Kind == "member_expression" && memberChain(callee):
		factory = text(callee.Fields["property"])
	}
	if routerFactories[factory] && len(args.Children) > 0 {
		bc.syn.RouterCalls = append(bc.syn.RouterCalls, facts.RouterCall{Callee: bc.site("", caller, owner, callee.Text, callee), Routes: bc.routeValue(args.Children[0], owner, caller, -1)})
	}
}

// unwrap removes parentheses, `as`, `satisfies` and `!` around a value.
func unwrap(n *parse.Node) *parse.Node {
	for n != nil && len(n.Children) > 0 && (n.Kind == "parenthesized_expression" || n.Kind == "as_expression" || n.Kind == "satisfies_expression" || n.Kind == "non_null_expression") {
		n = n.Children[0]
	}
	return n
}

// moduleConst records a module-level const declarator a route position or
// a JSX tag may name: route objects and arrays, and React.lazy bindings.
func (bc *bridgeCollector) moduleConst(c, statement *parse.Node) {
	name := c.Fields["name"]
	if name == nil || name.Kind != "identifier" {
		return
	}
	isConst := false
	for _, token := range statement.Tokens {
		isConst = isConst || token == "const"
	}
	if !isConst {
		return
	}
	value := unwrap(c.Fields["value"])
	if value == nil {
		return
	}
	switch value.Kind {
	case "object":
		if routeLike(value) {
			bc.consts(name.Text, bc.routeValue(value, "", "", -1))
		}
	case "array":
		if v := bc.routeValue(value, "", "", -1); routeArray(v) {
			bc.consts(name.Text, v)
		}
	case "call_expression":
		if module := lazyImport(value); module != "" {
			callee := value.Fields["function"]
			bc.syn.Lazy = append(bc.syn.Lazy, facts.LazyComponent{Name: name.Text, Callee: bc.site("", "", "", callee.Text, callee), Module: module})
		}
	}
}

func (bc *bridgeCollector) consts(name string, v facts.RouteValue) {
	if bc.syn.Consts == nil {
		bc.syn.Consts = map[string]facts.RouteValue{}
	}
	bc.syn.Consts[name] = v
}

// routeArray reports an array whose items are all objects, names or
// spreads of names.
func routeArray(v facts.RouteValue) bool {
	if v.Other || v.Candidate >= 0 || len(v.Items) == 0 {
		return false
	}
	for _, item := range v.Items {
		if item.Other {
			return false
		}
	}
	return true
}

// routeLike reports an object literal with at least one route key.
func routeLike(n *parse.Node) bool {
	for _, c := range n.Children {
		if routeKeys[propertyKey(c)] {
			return true
		}
	}
	return false
}

// propertyKey returns an object member's key as written ("" for spreads
// and computed keys).
func propertyKey(c *parse.Node) string {
	switch c.Kind {
	case "pair", "method_definition":
		key := c.Fields["key"]
		if c.Kind == "method_definition" {
			key = c.Fields["name"]
		}
		if key == nil {
			return ""
		}
		switch key.Kind {
		case "property_identifier", "identifier":
			return key.Text
		case "string":
			return literalString(key)
		}
	case "shorthand_property_identifier":
		return c.Text
	}
	return ""
}

// literalString returns a string or substitution-free template literal's
// raw content; "\x00" when the node is not one (or holds an escape).
func literalString(n *parse.Node) string {
	if n == nil || n.Kind != "string" && n.Kind != "template_string" {
		return "\x00"
	}
	if len(n.Children) == 0 {
		return ""
	}
	if len(n.Children) == 1 && n.Children[0].Kind == "string_fragment" {
		return n.Children[0].Text
	}
	return "\x00"
}

// lazyImport returns m for `lazy(() => import("m"))` (any callee: the
// linker proves it is React's lazy) and "" for any other call.
func lazyImport(call *parse.Node) string {
	callee := call.Fields["function"]
	args := call.Fields["arguments"]
	if callee == nil || args == nil || len(args.Children) != 1 || !(callee.Kind == "identifier" || callee.Kind == "member_expression" && memberChain(callee)) {
		return ""
	}
	fn := args.Children[0]
	if fn.Kind != "arrow_function" && fn.Kind != "function_expression" {
		return ""
	}
	return returnedImport(fn)
}

// returnedImport returns m when a function's only result is import("m").
func returnedImport(fn *parse.Node) string {
	body := fn.Fields["body"]
	if body != nil && body.Kind == "statement_block" {
		if len(body.Children) != 1 || body.Children[0].Kind != "return_statement" || len(body.Children[0].Children) != 1 {
			return ""
		}
		body = body.Children[0].Children[0]
	}
	return importCall(body)
}

// importCall returns m for import("m") (await and parentheses allowed).
func importCall(n *parse.Node) string {
	n = unwrap(n)
	for n != nil && n.Kind == "await_expression" && len(n.Children) == 1 {
		n = unwrap(n.Children[0])
	}
	if n == nil || n.Kind != "call_expression" || n.Fields["function"] == nil || n.Fields["function"].Kind != "import" {
		return ""
	}
	args := n.Fields["arguments"]
	if args == nil || len(args.Children) != 1 {
		return ""
	}
	if m := literalString(args.Children[0]); m != "\x00" && m != "" {
		return m
	}
	return ""
}

// routeValue reads a route position: an object candidate, an array of
// items, or another form.
func (bc *bridgeCollector) routeValue(n *parse.Node, owner, caller string, parent int) facts.RouteValue {
	n = unwrap(n)
	switch {
	case n == nil:
		return facts.RouteValue{Candidate: -1, Other: true}
	case n.Kind == "object":
		return facts.RouteValue{Candidate: bc.object(n, owner, caller, parent)}
	case n.Kind == "array":
		v := facts.RouteValue{Candidate: -1}
		for _, c := range n.Children {
			v.Items = append(v.Items, bc.routeItem(c, owner, caller, parent))
		}
		return v
	}
	return facts.RouteValue{Candidate: -1, Other: true}
}

func (bc *bridgeCollector) routeItem(c *parse.Node, owner, caller string, parent int) facts.RouteItem {
	c = unwrap(c)
	switch {
	case c.Kind == "object":
		return facts.RouteItem{Candidate: bc.object(c, owner, caller, parent)}
	case valueName(c) != "":
		s := bc.site("", caller, owner, c.Text, c)
		return facts.RouteItem{Candidate: -1, Ref: &s}
	case c.Kind == "spread_element" && len(c.Children) == 1 && valueName(c.Children[0]) != "":
		s := bc.site("", caller, owner, c.Children[0].Text, c.Children[0])
		return facts.RouteItem{Candidate: -1, Ref: &s, Spread: true}
	}
	return facts.RouteItem{Candidate: -1, Other: true}
}

// candidate appends a route candidate for n and returns its index.
func (bc *bridgeCollector) candidate(n *parse.Node, parent int) int {
	if i, ok := bc.objects[n]; ok {
		return i
	}
	i := len(bc.syn.Routes)
	bc.objects[n] = i
	bc.syn.Routes = append(bc.syn.Routes, facts.RouteCandidate{Span: span(n), ContentHash: contract.ContentHash(bc.source[n.Start:n.End]), Parent: parent, PathForm: "absent"})
	bc.starts = append(bc.starts, n.Start)
	return i
}

// object reads a route object literal (row RR1's keys).
func (bc *bridgeCollector) object(n *parse.Node, owner, caller string, parent int) int {
	if i, ok := bc.objects[n]; ok {
		return i
	}
	i := bc.candidate(n, parent)
	r := facts.RouteCandidate{Span: bc.syn.Routes[i].Span, ContentHash: bc.syn.Routes[i].ContentHash, Parent: parent, PathForm: "absent"}
	index := false
	for _, c := range n.Children {
		key := propertyKey(c)
		if key == "" {
			if c.Kind != "comment" {
				r.Open = true
			}
			continue
		}
		r.Present = append(r.Present, key)
		value := c.Fields["value"]
		if c.Kind == "shorthand_property_identifier" {
			value = c
		}
		switch key {
		case "path":
			if s := literalString(unwrap(value)); s != "\x00" {
				r.Path, r.PathForm = s, "literal"
			} else {
				r.PathForm = "dynamic"
			}
		case "index":
			index = value != nil && value.Kind == "true"
		case "Component", "loader", "action":
			if name := valueName(unwrap(value)); name != "" {
				r.Keys = setKey(r.Keys, key, bc.site("", caller, owner, name, unwrap(value)))
			}
		case "element":
			r.Element, r.ElementOther = bc.elementTags(value, owner, caller)
		case "lazy":
			l := bc.lazy(c, value)
			r.Lazy = &l
		case "children":
			if v := unwrap(value); v != nil && v.Kind == "array" {
				for _, item := range v.Children {
					r.Children = append(r.Children, bc.routeItem(item, owner, caller, i))
				}
			} else {
				r.Children = append(r.Children, facts.RouteItem{Candidate: -1, Other: true})
			}
		}
	}
	if index && r.PathForm == "absent" {
		r.PathForm = "index"
	}
	bc.syn.Routes[i] = r
	return i
}

func setKey(m map[string]facts.BridgeSite, key string, s facts.BridgeSite) map[string]facts.BridgeSite {
	if m == nil {
		m = map[string]facts.BridgeSite{}
	}
	m[key] = s
	return m
}

// elementTags returns the tags of a JSX element value outside nested
// functions; other marks a value of any other form.
func (bc *bridgeCollector) elementTags(value *parse.Node, owner, caller string) (tags []facts.BridgeSite, other bool) {
	value = unwrap(value)
	if value != nil && value.Kind == "jsx_expression" && len(value.Children) == 1 {
		value = unwrap(value.Children[0])
	}
	if value == nil || value.Kind != "jsx_element" && value.Kind != "jsx_self_closing_element" {
		return nil, true
	}
	var walk func(*parse.Node)
	walk = func(n *parse.Node) {
		switch n.Kind {
		case "arrow_function", "function_expression", "function", "generator_function":
			return
		case "jsx_opening_element", "jsx_self_closing_element":
			if name := tagName(n); name != "" && (strings.Contains(name, ".") || !intrinsicTag(name)) {
				tags = append(tags, bc.site("", caller, owner, name, n))
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(value)
	return tags, false
}

// lazy reads a route's lazy value (rows RR4/RR5): a function returning
// import("m"), or a function returning an object literal whose values are
// named exports of dynamically imported modules; everything else is
// "unknown".
func (bc *bridgeCollector) lazy(member, value *parse.Node) facts.RouteLazy {
	fn := unwrap(value)
	if member.Kind == "method_definition" {
		fn = member
	}
	out := facts.RouteLazy{Form: "unknown", Span: span(member)}
	if fn == nil || fn.Kind != "arrow_function" && fn.Kind != "function_expression" && fn.Kind != "method_definition" {
		return out
	}
	body := fn.Fields["body"]
	if body == nil {
		return out
	}
	if m := importCall(body); m != "" {
		return facts.RouteLazy{Form: "module", Module: m, Span: out.Span}
	}
	locals := map[string]facts.LazyImport{}
	result := unwrap(body)
	if body.Kind == "statement_block" {
		if countReturns(body) != 1 || len(body.Children) == 0 {
			return out
		}
		last := body.Children[len(body.Children)-1]
		if last.Kind != "return_statement" || len(last.Children) != 1 {
			return out
		}
		if m := importCall(last.Children[0]); m != "" {
			return facts.RouteLazy{Form: "module", Module: m, Span: out.Span}
		}
		result = unwrap(last.Children[0])
		for _, statement := range body.Children[:len(body.Children)-1] {
			if statement.Kind == "lexical_declaration" {
				for _, d := range statement.Children {
					if d.Kind == "variable_declarator" {
						lazyBindings(d, locals)
					}
				}
			}
		}
	}
	if result == nil || result.Kind != "object" {
		return out
	}
	props := map[string]facts.LazyImport{}
	for _, c := range result.Children {
		key := propertyKey(c)
		if key == "" || c.Kind == "method_definition" {
			if c.Kind != "comment" {
				return out
			}
			continue
		}
		value := c.Fields["value"]
		if c.Kind == "shorthand_property_identifier" {
			value = c
		}
		value = unwrap(value)
		switch {
		case value != nil && value.Kind == "identifier" || value != nil && value.Kind == "shorthand_property_identifier":
			if imp, ok := locals[value.Text]; ok {
				props[key] = imp
			} else {
				props[key] = facts.LazyImport{Unknown: true}
			}
		case value != nil && value.Kind == "member_expression" && importCall(value.Fields["object"]) != "" && value.Fields["property"] != nil && value.Fields["property"].Kind == "property_identifier":
			props[key] = facts.LazyImport{Module: importCall(value.Fields["object"]), Export: value.Fields["property"].Text}
		default:
			props[key] = facts.LazyImport{Unknown: true}
		}
	}
	return facts.RouteLazy{Form: "object", Props: props, Span: out.Span}
}

// countReturns counts the return statements of a function body outside
// nested functions.
func countReturns(n *parse.Node) int {
	count := 0
	for _, c := range n.Children {
		switch c.Kind {
		case "arrow_function", "function_expression", "function_declaration", "generator_function", "generator_function_declaration", "method_definition", "class_declaration", "class":
			continue
		case "return_statement":
			count++
		}
		count += countReturns(c)
	}
	return count
}

// lazyBindings reads `const { A: b } = await import("m")` and
// `const [{ A: b }, { C: d }] = await Promise.all([import("m1"), import("m2")])`.
func lazyBindings(d *parse.Node, locals map[string]facts.LazyImport) {
	name, value := d.Fields["name"], d.Fields["value"]
	if name == nil || value == nil {
		return
	}
	if m := importCall(value); m != "" && name.Kind == "object_pattern" {
		objectPattern(name, m, locals)
		return
	}
	v := unwrap(value)
	if v != nil && v.Kind == "await_expression" && len(v.Children) == 1 {
		v = unwrap(v.Children[0])
	}
	if v == nil || v.Kind != "call_expression" || text(v.Fields["function"]) != "Promise.all" || name.Kind != "array_pattern" {
		return
	}
	args := v.Fields["arguments"]
	if args == nil || len(args.Children) != 1 || args.Children[0].Kind != "array" {
		return
	}
	modules := args.Children[0].Children
	patterns := name.Children
	for i, p := range patterns {
		if i >= len(modules) {
			return
		}
		if m := importCall(modules[i]); m != "" && p.Kind == "object_pattern" {
			objectPattern(p, m, locals)
		}
	}
}

func objectPattern(p *parse.Node, module string, locals map[string]facts.LazyImport) {
	for _, c := range p.Children {
		switch c.Kind {
		case "shorthand_property_identifier_pattern":
			locals[c.Text] = facts.LazyImport{Module: module, Export: c.Text}
		case "pair_pattern":
			key, value := c.Fields["key"], c.Fields["value"]
			if key != nil && value != nil && key.Kind == "property_identifier" && value.Kind == "identifier" {
				locals[value.Text] = facts.LazyImport{Module: module, Export: key.Text}
			}
		}
	}
}

// routeElement reads a <Route> element's attributes (row RR2) when its tag
// is written Route or X.Route; it returns the candidate index or -1.
func (bc *bridgeCollector) routeElement(n *parse.Node, owner, caller string) int {
	opening := n
	if n.Kind == "jsx_element" {
		opening = n.Fields["open_tag"]
	}
	if opening == nil {
		return -1
	}
	name := tagName(opening)
	if name != "Route" && !strings.HasSuffix(name, ".Route") {
		return -1
	}
	parent := -1
	if len(bc.routes) > 0 {
		parent = bc.routes[len(bc.routes)-1]
	}
	i := bc.candidate(n, parent)
	r := facts.RouteCandidate{Span: bc.syn.Routes[i].Span, ContentHash: bc.syn.Routes[i].ContentHash, Parent: parent, PathForm: "absent", Tag: bc.site("", caller, owner, name, opening.Fields["name"])}
	index := false
	for _, a := range opening.Children {
		if a.Kind == "jsx_expression" {
			// {...props}: any key.
			r.Open = true
			continue
		}
		if a.Kind != "jsx_attribute" || len(a.Children) == 0 || a.Children[0].Kind != "property_identifier" {
			if a.Kind == "jsx_attribute" {
				r.Open = true
			}
			continue
		}
		key := a.Children[0].Text
		var value *parse.Node
		if len(a.Children) == 2 {
			value = a.Children[1]
		}
		r.Present = append(r.Present, key)
		inner := value
		if inner != nil && inner.Kind == "jsx_expression" && len(inner.Children) == 1 {
			inner = unwrap(inner.Children[0])
		}
		switch key {
		case "path":
			if s := literalString(inner); s != "\x00" {
				r.Path, r.PathForm = s, "literal"
			} else {
				r.PathForm = "dynamic"
			}
		case "index":
			index = value == nil || inner != nil && inner.Kind == "true"
		case "Component", "loader", "action":
			if name := valueName(inner); name != "" {
				r.Keys = setKey(r.Keys, key, bc.site("", caller, owner, name, inner))
			}
		case "element":
			r.Element, r.ElementOther = bc.elementTags(inner, owner, caller)
		case "lazy":
			l := bc.lazy(a, inner)
			r.Lazy = &l
		}
	}
	if index && r.PathForm == "absent" {
		r.PathForm = "index"
	}
	bc.syn.Routes[i] = r
	return i
}

// finish names the route candidates (identity.md: ui_route
// `<owner>.ROUTE <p>`, literal children under their parent, @N for
// repeats in source order) and returns the payload, nil when empty.
func (bc *bridgeCollector) finish(nodes []contract.Node) *facts.BridgeSyntax {
	fileID := id("file", bc.file, bc.file)
	bases := make([]string, len(bc.syn.Routes))
	for i := range bc.syn.Routes {
		r := &bc.syn.Routes[i]
		segment := "ROUTE " + routeSegment(*r)
		r.Owner = fileID
		bases[i] = segment
		if r.Parent >= 0 {
			bases[i] = bases[r.Parent] + "." + segment
		} else if owner := enclosing(nodes, r.Span); owner != nil {
			bases[i] = owner.QualifiedName + "." + segment
			r.Owner = owner.Identity
		}
		r.Display = segment
	}
	order := make([]int, len(bc.syn.Routes))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return bc.starts[order[a]] < bc.starts[order[b]] })
	seen := map[string]int{}
	for _, i := range order {
		seen[bases[i]]++
		r := &bc.syn.Routes[i]
		r.Qualified = bases[i]
		if n := seen[bases[i]]; n > 1 {
			r.Qualified += fmt.Sprintf("@%d", n)
			r.Display += fmt.Sprintf("@%d", n)
		}
	}
	// Parents are named before their children (index order), so the
	// owner identity of a literal child is its parent's final identity.
	for i := range bc.syn.Routes {
		if p := bc.syn.Routes[i].Parent; p >= 0 {
			bc.syn.Routes[i].Owner = id("ui_route", bc.file, bc.syn.Routes[p].Qualified)
		}
	}
	if len(bc.syn.Sites) == 0 && len(bc.syn.Lazy) == 0 && len(bc.syn.Routes) == 0 && len(bc.syn.RouterCalls) == 0 && len(bc.syn.Consts) == 0 {
		return nil
	}
	syn := bc.syn
	return &syn
}

// routeSegment is a route's path segment of its name.
func routeSegment(r facts.RouteCandidate) string {
	switch r.PathForm {
	case "literal":
		if r.Path == "" {
			return `""`
		}
		for _, c := range r.Path {
			if unicode.IsControl(c) {
				return "(dynamic)"
			}
		}
		return r.Path
	case "index":
		return "(index)"
	case "dynamic":
		return "(dynamic)"
	}
	return "(no path)"
}

// enclosing returns the innermost emitted declaration node whose span
// contains s.
func enclosing(nodes []contract.Node, s contract.Span) *contract.Node {
	var best *contract.Node
	for i := range nodes {
		n := &nodes[i]
		if n.Kind == "file" || n.Span == nil || !spanWithin(s, *n.Span) {
			continue
		}
		if best == nil || spanWithin(*n.Span, *best.Span) {
			best = n
		}
	}
	return best
}

func spanWithin(inner, outer contract.Span) bool {
	before := func(l1, c1, l2, c2 int) bool { return l1 < l2 || l1 == l2 && c1 <= c2 }
	return before(outer.StartLine, outer.StartColumn, inner.StartLine, inner.StartColumn) && before(inner.EndLine, inner.EndColumn, outer.EndLine, outer.EndColumn)
}

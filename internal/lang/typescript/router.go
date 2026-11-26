package typescript

import (
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// RouterSyntax defers Angular proof to linking without changing older syntax.
//
// Version 2 (ADR 0036 Revision 1) adds bootstrapApplication/createApplication
// calls with their options argument, module const objects with an own
// providers key (application configs) and the key names of methods in
// object literals that own a path or matcher key. Version 1 payloads miss.
type RouterSyntax struct {
	Version int                   `json:"version"`
	Calls   []RouterCall          `json:"calls"`
	Loaders []RouterLoader        `json:"loaders"`
	Arrays  []DeclarationConstant `json:"arrays"`
	Configs []DeclarationConstant `json:"configs"`
	Methods []RouterMethod        `json:"methods"`
}

// RouterSyntaxVersion is the current router payload version.
const RouterSyntaxVersion = 2

// RouterMethod names a method or accessor entry of a route-shaped object
// literal. DeclarationExpr keeps such entries without their key, so the
// router payload records it to tell "this key is present but not evaluated"
// apart from "any key may be hidden here".
type RouterMethod struct {
	Object contract.Span
	Index  int
	Key    string
}

// RouterCall preserves registration evidence even where older calls omit decorators.
type RouterCall struct {
	Offset         int
	Span           contract.Span
	Kind           string
	Callee         facts.RouteExpr
	Arguments      int
	First          *DeclarationExpr `json:",omitempty"`
	Scope          int
	Shadows        []string
	Receiver       *DeclarationExpr `json:",omitempty"`
	ReceiverCallee *facts.RouteExpr `json:",omitempty"`
	// Options is the ApplicationConfig argument of bootstrap_application
	// (second argument) and create_application (first argument).
	Options *DeclarationExpr `json:",omitempty"`
}

// RouterLoader distinguishes static loader targets from explicit syntax gaps.
type RouterLoader struct {
	Key             string
	Span, ValueSpan contract.Span
	Form, Reason    string
	Specifier       *string `json:",omitempty"`
	Member          string
	Value           *DeclarationExpr `json:",omitempty"`
	Scope           int
	Shadows         []string
}

// Valid rejects missing, obsolete and incomplete router payloads on replay.
func (s *RouterSyntax) Valid() bool {
	if s == nil || s.Version != RouterSyntaxVersion || s.Calls == nil || s.Loaders == nil || s.Arrays == nil || s.Configs == nil || s.Methods == nil {
		return false
	}
	for _, c := range s.Calls {
		if c.Offset < 0 || !routerSpanValid(c.Span) || c.Scope < 0 || c.Shadows == nil || !routerReferenceValid(c.Callee) || c.Arguments < 0 || (c.Arguments > 0) != (c.First != nil) {
			return false
		}
		if c.First != nil && !c.First.valid() || c.Options != nil && !c.Options.valid() {
			return false
		}
		switch c.Kind {
		case "provide_router", "for_root", "for_child", "bootstrap_application", "create_application":
		case "bootstrap_module":
			if c.Receiver == nil || !c.Receiver.valid() || c.Receiver.Kind == "call_expression" && (c.ReceiverCallee == nil || !routerReferenceValid(*c.ReceiverCallee)) {
				return false
			}
		default:
			return false
		}
	}
	for _, l := range s.Loaders {
		if (l.Key != "loadComponent" && l.Key != "loadChildren") || !routerSpanValid(l.Span) || !routerSpanValid(l.ValueSpan) || l.Scope < 0 || l.Shadows == nil {
			return false
		}
		switch l.Form {
		case "import_default":
			if l.Specifier == nil {
				return false
			}
		case "import_member":
			if l.Specifier == nil || l.Member == "" {
				return false
			}
		case "sync":
			if l.Value == nil || !l.Value.valid() {
				return false
			}
		case "inadmissible":
			if l.Reason == "" {
				return false
			}
		default:
			return false
		}
	}
	for _, a := range s.Arrays {
		if a.Name == "" || a.Value.Kind != "array" || !a.Value.valid() {
			return false
		}
	}
	for _, c := range s.Configs {
		if c.Name == "" || c.Value.Kind != "object" || !c.Value.valid() {
			return false
		}
	}
	for _, m := range s.Methods {
		if !routerSpanValid(m.Object) || m.Index < 0 || m.Key == "" {
			return false
		}
	}
	return true
}

func routerSpanValid(s contract.Span) bool {
	return s.StartLine > 0 && s.StartColumn >= 0 && s.EndLine >= s.StartLine && s.EndColumn >= 0 && (s.EndLine > s.StartLine || s.EndColumn > s.StartColumn)
}

func routerReferenceValid(e facts.RouteExpr) bool {
	if e.Kind == "" {
		return false
	}
	switch e.Kind {
	case "identifier", "property_identifier":
		return e.Name != ""
	case "member_expression":
		return e.Name != "" && e.Object != nil && routerReferenceValid(*e.Object)
	case "call_expression":
		return e.Object != nil && routerReferenceValid(*e.Object)
	}
	return true
}

// References omit source spelling and arguments duplicated by First/Receiver.
func routerReference(n *parse.Node) facts.RouteExpr {
	if n == nil {
		return facts.RouteExpr{}
	}
	e := facts.RouteExpr{Kind: n.Kind}
	switch n.Kind {
	case "identifier", "property_identifier":
		e.Name = n.Text
	case "member_expression":
		e.Name = text(n.Fields["property"])
		o := routerReference(n.Fields["object"])
		e.Object = &o
	case "call_expression":
		o := routerReference(n.Fields["function"])
		e.Object = &o
	}
	return e
}

func routerRoot(n *parse.Node) string {
	for n != nil && n.Kind == "member_expression" {
		n = n.Fields["object"]
	}
	if n != nil && n.Kind == "identifier" {
		return n.Text
	}
	return ""
}

func routerChildren(n *parse.Node) []*parse.Node {
	var out []*parse.Node
	if n != nil {
		for _, c := range n.Children {
			if c.Kind != "comment" {
				out = append(out, c)
			}
		}
	}
	return out
}

func routerUnwrap(n *parse.Node, assertions bool) *parse.Node {
	for n != nil && (n.Kind == "parenthesized_expression" || assertions && (n.Kind == "as_expression" || n.Kind == "satisfies_expression")) {
		var next *parse.Node
		for _, c := range n.Children {
			if c.Kind != "comment" && !strings.Contains(c.Kind, "type") {
				next = c
				break
			}
		}
		n = next
	}
	return n
}

func routerBody(n *parse.Node) (*parse.Node, string) {
	if n == nil {
		return nil, "not a function"
	}
	if n.Kind == "generator_function" {
		return nil, "generator"
	}
	if n.Kind != "arrow_function" && n.Kind != "function_expression" {
		return nil, "not a function"
	}
	for _, token := range n.Tokens {
		if token == "async" {
			return nil, "async"
		}
		if token == "*" {
			return nil, "generator"
		}
	}
	body := n.Fields["body"]
	if body != nil && body.Kind == "statement_block" {
		statements := routerChildren(body)
		if len(statements) != 1 || statements[0].Kind != "return_statement" {
			return nil, "other"
		}
		values := routerChildren(statements[0])
		if len(values) != 1 {
			return nil, "other"
		}
		body = values[0]
	}
	return routerUnwrap(body, false), ""
}

func routerImport(n *parse.Node) (string, bool) {
	n = routerUnwrap(n, false)
	if n == nil || n.Kind != "call_expression" || n.Fields["function"] == nil || n.Fields["function"].Kind != "import" {
		return "", false
	}
	args := routerChildren(n.Fields["arguments"])
	if len(args) != 1 {
		return "", false
	}
	v := args[0]
	if (v.Kind != "string" && v.Kind != "template_string") || len(v.Text) < 2 || strings.Contains(v.Text, `\`) {
		return "", false
	}
	for _, c := range v.Children {
		if c.Kind == "template_substitution" {
			return "", false
		}
	}
	return v.Text[1 : len(v.Text)-1], true
}

func routerLoader(n *parse.Node, key string) RouterLoader {
	v := n.Fields["value"]
	l := RouterLoader{Key: key, Span: span(n), ValueSpan: span(v), Form: "inadmissible", Shadows: []string{}}
	body, reason := routerBody(v)
	l.Reason = reason
	if key == "loadChildren" && v.Kind == "string" {
		l.Reason = "string loadChildren"
	}
	if reason != "" {
		return l
	}
	if specifier, ok := routerImport(body); ok {
		l.Form, l.Reason, l.Specifier = "import_default", "", &specifier
		return l
	}
	if routerRoot(body) != "" {
		l.Form, l.Reason = "sync", ""
		e := declarationExpr(body)
		l.Value = &e
		return l
	}
	l.Reason = "other"
	if body == nil || body.Kind != "call_expression" {
		return l
	}
	callee := body.Fields["function"]
	if callee == nil {
		return l
	}
	if callee.Kind == "import" {
		l.Reason = "dynamic specifier"
		return l
	}
	if callee.Kind != "member_expression" || text(callee.Fields["property"]) != "then" {
		return l
	}
	args := routerChildren(body.Fields["arguments"])
	l.Reason = "then arguments"
	if len(args) != 1 {
		return l
	}
	fn := args[0]
	result, reason := routerBody(fn)
	if reason != "" {
		l.Reason = reason
		return l
	}
	params := routerChildren(fn.Fields["parameters"])
	if p := fn.Fields["parameter"]; p != nil {
		params = []*parse.Node{p}
	}
	if len(params) == 1 && params[0].Kind == "required_parameter" {
		// Type annotations do not change the identifier binding pattern.
		// Defaulted, optional and rest patterns remain outside this form.
		p := params[0]
		if p.Fields["value"] == nil {
			params[0] = p.Fields["pattern"]
		}
	}
	l.Reason = "then destructuring"
	if len(params) != 1 || params[0] == nil || params[0].Kind != "identifier" {
		return l
	}
	l.Reason = "other"
	if result == nil || result.Kind != "member_expression" || result.Fields["object"] == nil || result.Fields["object"].Kind != "identifier" || result.Fields["object"].Text != params[0].Text || result.Fields["property"] == nil || result.Fields["property"].Kind != "property_identifier" {
		return l
	}
	l.Reason = "dynamic specifier"
	if specifier, ok := routerImport(callee.Fields["object"]); ok {
		l.Form, l.Reason, l.Specifier, l.Member = "import_member", "", &specifier, result.Fields["property"].Text
	}
	return l
}

// CaptureRouter adds only source syntax; Angular resolution belongs to T014d2.
func CaptureRouter(root *parse.Node, f Facts, declarations DeclarationSyntax) RouterSyntax {
	s := RouterSyntax{Version: RouterSyntaxVersion, Calls: []RouterCall{}, Loaders: []RouterLoader{}, Arrays: []DeclarationConstant{}, Configs: []DeclarationConstant{}, Methods: []RouterMethod{}}
	imports := map[string]string{}
	for _, imp := range f.Imports {
		for local, exported := range imp.Bindings {
			imports[local] = exported
		}
	}
	constants := map[string]bool{}
	for _, c := range declarations.Constants {
		constants[c.Name] = true
	}
	// A full span disambiguates chains that share their starting byte offset (F5).
	calls := map[contract.Span]facts.ClientCall{}
	if f.Routes != nil {
		for _, c := range f.Routes.Clients {
			if c.Expression.Kind == "call_expression" {
				calls[c.Span] = c
			}
		}
	}
	var ancestors []*parse.Node
	var walk func(*parse.Node, bool)
	walk = func(n *parse.Node, module bool) {
		if n == nil {
			return
		}
		if n.Kind == "call_expression" {
			callee := n.Fields["function"]
			kind := routerCallKind(callee, imports)
			if kind != "" {
				args := routerChildren(n.Fields["arguments"])
				var first, options *parse.Node
				if len(args) > 0 {
					first = args[0]
				}
				switch {
				case kind == "bootstrap_application" && len(args) > 1:
					options = args[1]
				case kind == "create_application":
					options = first
				}
				scope, shadows := routerScope(f, calls, n, callee, first, options, ancestors)
				c := RouterCall{Offset: int(n.Start), Span: span(n), Kind: kind, Callee: routerReference(callee), Arguments: len(args), Scope: scope, Shadows: shadows}
				if first != nil {
					e := declarationExpr(first)
					c.First = &e
				}
				if options != nil {
					e := declarationExpr(options)
					c.Options = &e
				}
				if kind == "bootstrap_module" {
					receiver := callee.Fields["object"]
					e := declarationExpr(receiver)
					c.Receiver = &e
					if receiver != nil && receiver.Kind == "call_expression" {
						e := routerReference(receiver.Fields["function"])
						c.ReceiverCallee = &e
					}
				}
				s.Calls = append(s.Calls, c)
			}
		}
		if n.Kind == "pair" && n.Fields["value"] != nil {
			key := n.Fields["key"]
			name := ""
			if key != nil {
				if key.Kind == "identifier" || key.Kind == "property_identifier" {
					name = key.Text
				}
				if key.Kind == "string" {
					name, _ = jsString(key.Text)
				}
			}
			if name == "loadComponent" || name == "loadChildren" {
				l := routerLoader(n, name)
				if l.Form == "sync" {
					body, _ := routerBody(n.Fields["value"])
					loaderAncestors := append(ancestors[:len(ancestors):len(ancestors)], n.Fields["value"])
					l.Scope, l.Shadows = routerScope(f, nil, n, nil, body, nil, loaderAncestors)
				}
				s.Loaders = append(s.Loaders, l)
			}
		}
		if n.Kind == "object" {
			routerMethods(n, &s)
		}
		if module {
			routerArrays(n, constants, &s)
			routerConfigs(n, &s)
		}
		ancestors = append(ancestors, n)
		childModule := module && (n.Kind == "program" || n.Kind == "export_statement")
		for _, c := range n.Children {
			walk(c, childModule)
		}
		ancestors = ancestors[:len(ancestors)-1]
	}
	walk(root, true)
	return s
}

func routerCallKind(callee *parse.Node, imports map[string]string) string {
	if callee == nil {
		return ""
	}
	if callee.Kind == "identifier" {
		if kind := routerImportKinds[imports[callee.Text]]; kind != "" {
			return kind
		}
	}
	if callee.Kind != "member_expression" {
		return ""
	}
	object, property := callee.Fields["object"], callee.Fields["property"]
	if property == nil || property.Kind != "property_identifier" {
		return ""
	}
	switch property.Text {
	case "provideRouter", "bootstrapApplication", "createApplication":
		if object != nil && object.Kind == "identifier" && imports[object.Text] == "*" {
			return routerImportKinds[property.Text]
		}
	case "forRoot":
		if imports[routerRoot(object)] != "" {
			return "for_root"
		}
	case "forChild":
		if imports[routerRoot(object)] != "" {
			return "for_child"
		}
	case "bootstrapModule":
		return "bootstrap_module"
	}
	return ""
}

// routerImportKinds maps imported function names to call kinds. Proof that
// the import is the Angular export happens at link time.
var routerImportKinds = map[string]string{
	"provideRouter":        "provide_router",
	"bootstrapApplication": "bootstrap_application",
	"createApplication":    "create_application",
}

// routerConfigs keeps module single-declarator const object literals with an
// own providers key: the ApplicationConfig shape passed to bootstrapApplication.
func routerConfigs(n *parse.Node, s *RouterSyntax) {
	if n.Kind != "lexical_declaration" {
		return
	}
	constant := false
	for _, token := range n.Tokens {
		constant = constant || token == "const"
	}
	var ds []*parse.Node
	for _, child := range n.Children {
		if child.Kind == "variable_declarator" {
			ds = append(ds, child)
		}
	}
	if !constant || len(ds) != 1 {
		return
	}
	name, value := ds[0].Fields["name"], routerUnwrap(ds[0].Fields["value"], true)
	if name == nil || name.Kind != "identifier" || value == nil || value.Kind != "object" {
		return
	}
	for _, p := range value.Children {
		key := p.Fields["key"]
		own := p.Kind == "shorthand_property_identifier" && p.Text == "providers"
		if p.Kind == "pair" && key != nil {
			k := key.Text
			if key.Kind == "string" {
				k, _ = jsString(key.Text)
			} else if key.Kind != "identifier" && key.Kind != "property_identifier" {
				k = ""
			}
			own = k == "providers"
		}
		if own {
			s.Configs = append(s.Configs, DeclarationConstant{Name: name.Text, Value: declarationExpr(value)})
			return
		}
	}
}

// routerMethods records the key names of method and accessor entries of an
// object literal that owns a path or matcher pair key.
func routerMethods(n *parse.Node, s *RouterSyntax) {
	entries := []*parse.Node{}
	routeShaped := false
	for _, p := range n.Children {
		if p.Kind == "comment" {
			continue
		}
		entries = append(entries, p)
		if key := p.Fields["key"]; p.Kind == "pair" && key != nil {
			k := key.Text
			if key.Kind == "string" {
				k, _ = jsString(key.Text)
			} else if key.Kind != "identifier" && key.Kind != "property_identifier" {
				k = ""
			}
			routeShaped = routeShaped || k == "path" || k == "matcher"
		}
	}
	if !routeShaped {
		return
	}
	for i, p := range entries {
		if p.Kind != "method_definition" || p.Fields["name"] == nil {
			continue
		}
		name := p.Fields["name"]
		key := ""
		switch name.Kind {
		case "property_identifier", "identifier":
			key = name.Text
		case "string":
			key, _ = jsString(name.Text)
		}
		if key != "" {
			s.Methods = append(s.Methods, RouterMethod{Object: span(n), Index: i, Key: key})
		}
	}
}

func routerArrays(n *parse.Node, constants map[string]bool, s *RouterSyntax) {
	if n.Kind == "export_statement" {
		if v := routerUnwrap(n.Fields["value"], true); v != nil && v.Kind == "array" {
			s.Arrays = append(s.Arrays, DeclarationConstant{Name: "default", Value: declarationExpr(v)})
		}
	}
	if n.Kind != "lexical_declaration" {
		return
	}
	constant := false
	for _, token := range n.Tokens {
		constant = constant || token == "const"
	}
	var ds []*parse.Node
	for _, child := range n.Children {
		if child.Kind == "variable_declarator" {
			ds = append(ds, child)
		}
	}
	if !constant || len(ds) != 1 {
		return
	}
	d := ds[0]
	name, value := d.Fields["name"], d.Fields["value"]
	if name == nil || name.Kind != "identifier" || constants[name.Text] {
		return
	}
	satisfies := false
	for v := value; v != nil && (v.Kind == "parenthesized_expression" || v.Kind == "as_expression" || v.Kind == "satisfies_expression"); {
		satisfies = satisfies || v.Kind == "satisfies_expression"
		children := routerChildren(v)
		if len(children) == 0 {
			break
		}
		v = children[0]
	}
	if v := routerUnwrap(value, true); satisfies && v != nil && v.Kind == "array" {
		s.Arrays = append(s.Arrays, DeclarationConstant{Name: name.Text, Value: declarationExpr(v)})
	}
}

func routerScope(f Facts, calls map[contract.Span]facts.ClientCall, n, callee, first, options *parse.Node, ancestors []*parse.Node) (int, []string) {
	used := map[string]bool{routerScopeRoot(callee): true, routerScopeRoot(first): true}
	if callee != nil {
		// Calls may pass inline route objects with references below the root.
		// Sync loaders retain their existing root-only shadow semantics.
		used = map[string]bool{routerScopeRoot(callee): true}
		routerArgumentNames(first, used)
		routerArgumentNames(options, used)
	}
	names := map[string]bool{}
	for _, at := range ancestors {
		if declarationScope(at.Kind) {
			routerShadows(at, names)
		}
	}
	shadows := []string{}
	for name := range used {
		if name != "" && names[name] {
			shadows = append(shadows, name)
		}
	}
	sort.Strings(shadows)
	if c, ok := calls[span(n)]; ok && c.Expression.Object != nil && callee != nil && c.Expression.Object.Text == callee.Text && f.Routes != nil && c.Scope >= 0 && c.Scope < len(f.Routes.Scopes) {
		// Historical scopes have no declaration spans to prove which of several
		// same-name local shadows they bind. Keep those roots explicit instead.
		correct := len(shadows) == 0
		for name := range used {
			if name == "" {
				continue
			}
			local := false
			for at := c.Scope; at >= 0 && at < len(f.Routes.Scopes); at = f.Routes.Scopes[at].Parent {
				if _, found := f.Routes.Scopes[at].Bindings[name]; found {
					local = at != 0
					break
				}
			}
			if local != names[name] {
				correct = false
			}
		}
		if correct {
			return c.Scope, []string{}
		}
	}
	return 0, shadows
}

func routerArgumentNames(n *parse.Node, names map[string]bool) {
	if n == nil || strings.Contains(n.Kind, "type") && n.Kind != "type_assertion" {
		return
	}
	if n.Kind == "identifier" || n.Kind == "shorthand_property_identifier" {
		names[n.Text] = true
	}
	for _, child := range n.Children {
		if n.Kind == "pair" && child == n.Fields["key"] || n.Kind == "member_expression" && child == n.Fields["property"] {
			continue
		}
		routerArgumentNames(child, names)
	}
}

func routerScopeRoot(n *parse.Node) string {
	for n != nil {
		n = routerUnwrap(n, true)
		if n == nil {
			break
		}
		switch n.Kind {
		case "member_expression", "subscript_expression":
			n = n.Fields["object"]
		case "call_expression":
			n = n.Fields["function"]
		case "identifier":
			return n.Text
		default:
			return ""
		}
	}
	return ""
}

func routerPattern(n *parse.Node, names map[string]bool) {
	if n == nil {
		return
	}
	switch n.Kind {
	case "identifier", "type_identifier", "shorthand_property_identifier_pattern":
		names[n.Text] = true
	case "required_parameter", "optional_parameter":
		routerPattern(n.Fields["pattern"], names)
	case "assignment_pattern", "object_assignment_pattern":
		routerPattern(n.Fields["left"], names)
	case "pair_pattern":
		routerPattern(n.Fields["value"], names)
	case "object_pattern", "array_pattern", "rest_pattern", "formal_parameters":
		for _, c := range n.Children {
			routerPattern(c, names)
		}
	}
}

func routerShadows(scope *parse.Node, names map[string]bool) {
	routerPattern(scope.Fields["parameters"], names)
	routerPattern(scope.Fields["parameter"], names)
	if scope.Kind == "for_in_statement" {
		for _, token := range scope.Tokens {
			if token == "const" || token == "let" || token == "var" {
				routerPattern(scope.Fields["left"], names)
			}
		}
	}
	if scope.Kind == "function_expression" {
		routerPattern(scope.Fields["name"], names)
	}
	var walk func(*parse.Node, bool)
	walk = func(n *parse.Node, nested bool) {
		if n == nil {
			return
		}
		if n != scope && (n.Kind == "class_declaration" || n.Kind == "abstract_class_declaration" || n.Kind == "function_declaration") {
			if !nested {
				routerPattern(n.Fields["name"], names)
			}
			return
		}
		if n != scope && declarationScope(n.Kind) {
			if n.Kind != "statement_block" && n.Kind != "for_statement" && n.Kind != "for_in_statement" {
				return
			}
			nested = true
		}
		if n.Kind == "for_in_statement" {
			for _, token := range n.Tokens {
				if token == "var" {
					routerPattern(n.Fields["left"], names)
				}
			}
		}
		if n.Kind == "variable_declaration" || !nested && n.Kind == "lexical_declaration" {
			for _, d := range n.Children {
				if d.Kind == "variable_declarator" {
					routerPattern(d.Fields["name"], names)
				}
			}
		}
		for _, c := range n.Children {
			walk(c, nested)
		}
	}
	if body := scope.Fields["body"]; body != nil {
		// The function's body is its own scope, not an additional nested block.
		for _, c := range body.Children {
			walk(c, false)
		}
		if scope.Kind == "for_statement" || scope.Kind == "for_in_statement" {
			for _, c := range scope.Children {
				if c != body {
					walk(c, false)
				}
			}
		}
	} else {
		walk(scope, false)
	}
}

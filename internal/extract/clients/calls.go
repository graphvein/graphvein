package clients

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/routes"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/testscope"
)

type request struct {
	angular     angularBase
	library     string
	url, method expr
	object      object
	config      expr
	wrapper     string
	// swr marks a request made through an SWR hook (row NX-L2): library
	// keeps the transport (fetch or axios) for URL and base rules, and the
	// client call node reports library swr.
	swr bool
}

func defaultMethod() expr { return expr{Kind: "literal", Name: "GET", Text: "GET"} }
func configMethod(config expr) expr {
	if config.Kind == "" {
		return defaultMethod()
	}
	if config.Kind != "object" {
		return expr{Kind: "dynamic", Text: config.Text}
	}
	if v, ok := property(config, "method"); ok {
		return v
	}
	return defaultMethod()
}
func arg(args []expr, i int) expr {
	if i < len(args) {
		return args[i]
	}
	return expr{}
}

func (r *resolver) recognize(file string, c facts.ClientCall) (request, bool) {
	if q, ok := r.heyRequest(file, c); ok {
		return q, true
	}
	if q, ok := r.angularRequest(file, c); ok {
		return q, true
	}
	e := c.Expression
	if e.Object == nil {
		return request{}, false
	}
	f := *e.Object
	fetch := false
	if f.Kind == "identifier" && f.Name == "fetch" {
		_, _, bound := r.binding(file, c.Scope, "fetch")
		fetch = !bound
		for _, n := range r.files[file].Nodes {
			if n.QualifiedName == "fetch" {
				fetch = false
			}
		}
	}
	if f.Kind == "member_expression" && f.Name == "fetch" && f.Object != nil && f.Object.Kind == "identifier" {
		name := f.Object.Name
		_, _, bound := r.binding(file, c.Scope, name)
		fetch = !bound && (name == "window" || name == "globalThis" || name == "self")
	}
	if fetch {
		return request{library: "fetch", url: arg(e.Args, 0), method: configMethod(arg(e.Args, 1)), config: arg(e.Args, 1)}, true
	}
	receiver := f
	verb := ""
	if f.Kind == "member_expression" && f.Object != nil {
		receiver = *f.Object
		verb = f.Name
	}
	v := r.resolve(file, c.Scope, receiver, 0, nil)
	if !isAxios(v) {
		return request{}, false
	}
	q := request{library: "axios", object: v}
	switch verb {
	case "get", "delete", "head", "options", "post", "put", "patch", "postForm", "putForm", "patchForm":
		q.url = arg(e.Args, 0)
		i := 1
		if strings.HasPrefix(verb, "post") || strings.HasPrefix(verb, "put") || strings.HasPrefix(verb, "patch") {
			i = 2
		}
		q.config = arg(e.Args, i)
		q.method = expr{Kind: "literal", Name: strings.ToUpper(strings.TrimSuffix(verb, "Form"))}
	case "request", "":
		q.config = arg(e.Args, 0)
		if verb == "" && len(e.Args) > 0 && e.Args[0].Kind != "object" {
			// A literal/const first argument or a second config is the URL form.
			_, literal := r.literal(file, c.Scope, e.Args[0], nil)
			if literal || len(e.Args) > 1 || e.Args[0].Kind == "template_string" || e.Args[0].Kind == "binary_expression" {
				q.url = e.Args[0]
				q.config = arg(e.Args, 1)
			}
		}
		if q.url.Kind == "" {
			if url, ok := property(q.config, "url"); ok {
				q.url = url
			} else {
				q.url = expr{Kind: "dynamic", Text: q.config.Text}
			}
		}
		q.method = configMethod(q.config)
	default:
		return request{}, false
	}
	return q, true
}

type wrapper struct {
	request request
	params  []facts.ClientParameter
	depth   int
	// file and scope locate the wrapper's request site, so an invocation
	// can be checked to see the wrapper's own binding (row LF-3).
	file  string
	scope int
}

// invokes reports whether call, whose resolved callee is w, reaches w
// through w's own binding (row LF-3). A call through a plain identifier in
// the wrapper's file must resolve, in its lexical scopes, to the binding
// that declares the wrapper: a destructured or parameter binding of the
// same name shadows it, and a function declared inside another function is
// not visible outside it, although its identity does not say so. Member
// calls and calls from other files keep the resolved callee.
func (r *resolver) invokes(file string, call facts.ClientCall, w wrapper) bool {
	callee := call.Expression.Object
	if callee == nil || callee.Kind != "identifier" || file != w.file {
		linkTag("LF-U")
		return true
	}
	_, declared, ok := r.binding(w.file, w.scope, callee.Name)
	if !ok {
		linkTag("LF-U")
		return true
	}
	_, at, bound := r.binding(file, call.Scope, callee.Name)
	if bound && at == declared {
		linkTag("LF-U")
		return true
	}
	linkTag("LF-3")
	return false
}

func substitute(e expr, values map[string]expr) expr {
	return substituteValue(e, values, false)
}

func substituteValue(e expr, values map[string]expr, fetch bool) expr {
	if e.Kind == "identifier" {
		if v, ok := values[e.Name]; ok {
			return v
		}
	}
	if e.Object != nil {
		v := substituteValue(*e.Object, values, fetch)
		e.Object = &v
	}
	if e.Kind == "forwarded_options" && e.Object != nil && (e.Object.Kind == "object" || e.Object.Kind == "" || e.Object.Kind == "forwarded_options" || e.Object.Kind == "fetch_null" || e.Object.Kind == "fetch_undefined") {
		return *e.Object
	}
	if e.Kind == "member_expression" && e.Object != nil && e.Object.Kind == "object" {
		if fetch && e.Name == "method" {
			return expr{Kind: "fetch_options_method", Object: e.Object, Text: e.Text}
		}
		if v, ok := property(*e.Object, e.Name); ok {
			return v
		}
		return expr{}
	}
	e.Parts = append([]expr(nil), e.Parts...)
	for i := range e.Parts {
		e.Parts[i] = substituteValue(e.Parts[i], values, fetch)
	}
	if e.Properties != nil {
		p := map[string]expr{}
		for k, v := range e.Properties {
			p[k] = substituteValue(v, values, fetch)
		}
		e.Properties = p
	}
	e.Keywords = append([]facts.RouteKeyword(nil), e.Keywords...)
	for i := range e.Keywords {
		e.Keywords[i].Value = substituteValue(e.Keywords[i].Value, values, fetch)
	}
	if e.Kind == "binary_expression" && len(e.Parts) == 2 && (e.Operator == "??" || e.Operator == "||") {
		if e.Parts[0].Kind == "literal" {
			if e.Operator == "||" && e.Parts[0].Name == "" {
				return e.Parts[1]
			}
			return e.Parts[0]
		}
		if e.Parts[0].Kind == "" || e.Parts[0].Kind == "null" {
			return e.Parts[1]
		}
	}
	return e
}
func (w wrapper) apply(args []expr, identity string) request {
	values := map[string]expr{}
	for i, p := range w.params {
		v := arg(args, i)
		defaulted := false
		if v.Kind == "" || w.request.library == "fetch" && v.Kind == "undefined" {
			v = p.Default
			defaulted = true
		}
		provenEmptyDefault := defaulted && (v.Kind == "fetch_null" || v.Kind == "fetch_undefined")
		if w.request.library == "fetch" && spreadReferences(w.request.config, p.Name) && v.Kind != "object" && v.Kind != "" && !provenEmptyDefault {
			copy := v
			v = expr{Kind: "forwarded_options", Object: &copy, Text: copy.Text}
		}
		values[p.Name] = v
	}
	q := w.request
	q.url = substitute(q.url, values)
	q.method = substitute(q.method, values)
	q.config = substituteValue(q.config, values, q.library == "fetch")
	q.wrapper = identity
	return q
}
func (r *resolver) wrapperShape(file string, c facts.ClientCall, q request, params []facts.ClientParameter) bool {
	parameter := func(e expr) bool {
		if e.Kind == "forwarded_options" && e.Object != nil {
			e = *e.Object
		}
		if e.Kind != "identifier" {
			return false
		}
		for _, p := range params {
			if p.Name == e.Name && p.Name != "" {
				binding, at, ok := r.binding(file, c.Scope, e.Name)
				return ok && binding.Value.Kind == "parameter" && at == r.files[file].Routes.ParameterScopes[c.Owner]
			}
		}
		return false
	}
	pieces := r.pieces(file, c.Scope, q.url)
	if len(pieces) == 0 || !parameter(pieces[len(pieces)-1].expression) {
		return false
	}
	if q.wrapper != "" && len(pieces) > 1 && pieces[0].expression.Kind == "environment_base" {
		// For newly admitted environment wrappers, propagate only whole URL
		// parameters. A resource helper composing an ID into a path remains a
		// request intent; treating it as a transport would hide its route edge.
		forwarded := false
		for _, argument := range c.Expression.Args {
			forwarded = forwarded || parameter(argument) && argument.Name == pieces[len(pieces)-1].expression.Name
		}
		if !forwarded {
			return false
		}
	}
	for i, p := range pieces[:len(pieces)-1] {
		if !p.literal && !(q.library == "fetch" && i == 0 && r.baseShaped(file, c.Scope, p.expression)) {
			return false
		}
	}
	var method func(expr) bool
	method = func(e expr) bool {
		if _, ok := r.literal(file, c.Scope, e, nil); ok {
			return true
		}
		if parameter(e) {
			return true
		}
		if e.Kind == "undefined" {
			values := newEvaluation(r).expression(evaluationContext{file: file, scope: c.URLScope}, e)
			return len(values) == 1 && values[0].kind == "undefined"
		}
		if e.Kind == "member_expression" && e.Name == "method" && e.Object != nil && parameter(*e.Object) {
			return true
		}
		return e.Kind == "binary_expression" && len(e.Parts) == 2 && (e.Operator == "??" || e.Operator == "||") && method(e.Parts[0]) && e.Parts[1].Kind == "literal"
	}
	if q.library == "fetch" && q.config.Kind == "object" {
		for _, entry := range q.config.Keywords {
			if entry.Value.Kind == "spread_element" {
				// A spread is evaluated per invocation, including unknown
				// sources. Recognition must not turn that uncertainty into GET
				// or discard the invocation node altogether.
				return true
			}
		}
		if q.wrapper != "" && r.fetchMethod(file, c.URLScope, q.config).Kind == "literal" {
			return true
		}
		return method(configMethod(q.config))
	}
	return method(q.method)
}

// Link recognizes calls using the same module inventory and existing callable edges.
// Configuration and current route paths are applied here, never in cached syntax.
func Link(files map[string]facts.Facts, modules *typescript.ModuleResolver, routes []contract.Node, callEdges []contract.Edge, basePaths map[string]string, scopeRules ...testscope.Rules) ([]contract.Node, []contract.Edge) {
	nodes, edges, _ := LinkWithSources(files, modules, routes, callEdges, basePaths, nil, scopeRules...)
	return nodes, edges
}

// LinkWithSources rereads Angular application metadata on cache replay.
func LinkWithSources(files map[string]facts.Facts, modules *typescript.ModuleResolver, routes []contract.Node, callEdges []contract.Edge, basePaths map[string]string, sources map[string][]byte, scopeRules ...testscope.Rules) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	return LinkWithRoot(files, modules, routes, callEdges, Options{BasePaths: basePaths}, sources, "", scopeRules...)
}

// Options are the graphvein.yaml clients settings the link pass applies.
type Options struct {
	// BasePaths supplies explicit bases where source syntax cannot.
	BasePaths map[string]string
	// LinkGeneratedOutput links the client calls of committed bundler
	// output too (ADR 0085's opt-in): by default they are not linked.
	LinkGeneratedOutput bool
}

// LinkWithRoot permits one output-directory stat when scanner visibility alone
// cannot distinguish absent generated output from excluded generated output.
func LinkWithRoot(files map[string]facts.Facts, modules *typescript.ModuleResolver, routes []contract.Node, callEdges []contract.Edge, options Options, sources map[string][]byte, root string, scopeRules ...testscope.Rules) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	basePaths := options.BasePaths
	rules := testscope.Rules{}
	if len(scopeRules) > 0 {
		rules = scopeRules[0]
	}
	r := resolver{files: files, modules: modules, defaults: map[string][]baseAssignment{}, memo: map[string]memoValue{}}
	diagnostics := r.setupAngular(sources, rules)
	diagnostics = append(diagnostics, r.setupHeyAPI(sources, rules, root)...)
	for _, file := range facts.Paths(files) {
		if d := files[file].Routes; d != nil {
			for _, a := range d.Defaults {
				v := r.resolve(file, a.Scope, a.Receiver, 0, nil)
				if isAxios(v) {
					key := objectKey(v)
					text, ok := r.literal(file, a.Scope, a.Value, nil)
					kind := "unresolved"
					if ok {
						kind = "literal"
						if a.Value.Kind != "literal" {
							kind = "constant"
						}
					}
					r.defaults[key] = append(r.defaults[key], baseAssignment{text, kind, compact(a.Value.Text)})
				}
			}
		}
	}
	// Index supporting call evidence by site. No new callable inference is introduced.
	targets := map[string]string{}
	for _, e := range callEdges {
		if e.Kind == "calls" {
			for _, v := range e.Evidence {
				targets[siteKey(v.File, v.Span)] = e.Target
			}
		}
	}
	wrappers := map[string]wrapper{}
	for _, file := range facts.Paths(files) {
		if d := files[file].Routes; d != nil {
			for _, c := range d.Clients {
				if target := r.angularTarget(file, c); target != "" {
					targets[siteKey(file, c.Span)] = target
				}
			}
		}
	}
	requests := map[string]request{}
	for depth := 1; depth <= 3; depth++ {
		pending := map[string]wrapper{}
		invalid := map[string]bool{}
		for _, file := range facts.Paths(files) {
			d := files[file].Routes
			if d == nil {
				continue
			}
			for _, c := range d.Clients {
				q, ok := r.recognize(file, c)
				if !ok {
					target := targets[siteKey(file, c.Span)]
					if w, found := wrappers[target]; found && w.depth < depth && r.invokes(file, c, w) {
						q = r.applyWrapper(file, c, w, target)
						ok = true
					}
				}
				if !ok {
					continue
				}
				requests[siteKey(file, c.Span)] = q
				params, has := d.Parameters[c.Owner]
				if !has || !r.wrapperShape(file, c, q, params) {
					continue
				}
				params = append([]facts.ClientParameter(nil), params...)
				if _, exists := wrappers[c.Owner]; exists {
					continue
				}
				if _, exists := pending[c.Owner]; exists {
					invalid[c.Owner] = true
				}
				q.url = r.freeze(file, c.Scope, q.url)
				q.method = r.freeze(file, c.Scope, q.method)
				if q.library == "fetch" {
					q.url = r.freezeEnvironmentBase(file, c.Scope, q.url)
					q.config = r.freezeFetchConfig(file, c.URLScope, q.config, params)
					defaultScope := c.URLScope
					for defaultScope > 0 && !d.URLScopes[defaultScope].Function {
						defaultScope = d.URLScopes[defaultScope].Parent
					}
					if defaultScope > 0 {
						defaultScope = d.URLScopes[defaultScope].Parent
					}
					// Initializers run before body declarations exist. Parameters
					// cannot be proven here, including earlier parameters.
					defaultEnv := &environment{values: map[string]localValue{}}
					for _, p := range params {
						defaultEnv.values[p.Name] = localValue{values: valueSet{unknownValue(expr{Kind: "identifier", Name: p.Name})}}
					}
					defaultContext := evaluationContext{file: file, scope: defaultScope, env: defaultEnv}
					for i := range params {
						params[i].Default = r.freezeFetchDefault(defaultContext, params[i].Default)
					}
				}
				pending[c.Owner] = wrapper{q, params, depth, file, c.Scope}
			}
		}
		for identity, w := range pending {
			if !invalid[identity] {
				wrappers[identity] = w
			}
		}
	}
	var nodes []contract.Node
	var edges []contract.Edge
	diagnostics = append(diagnostics, r.heyOverrides(requests)...)
	duplicates := map[string]int{}
	matcher := newMatcher(routes, routesProof(files, modules, routes)).withNextOrigins(nextOrigins(sources))
	for _, file := range facts.Paths(files) {
		d := files[file].Routes
		if d == nil {
			continue
		}
		// Committed bundler output (ADR 0085): its calls are recorded
		// but not linked, unless graphvein.yaml opts in.
		var bundle *BundleMarker
		if !options.LinkGeneratedOutput {
			bundle = bundleMarker(file, sources[file])
		}
		bundleCalls := 0
		calls := append([]facts.ClientCall(nil), d.Clients...)
		sort.SliceStable(calls, func(i, j int) bool {
			a, b := calls[i].Span, calls[j].Span
			if a.StartLine != b.StartLine {
				return a.StartLine < b.StartLine
			}
			if a.StartColumn != b.StartColumn {
				return a.StartColumn < b.StartColumn
			}
			if a.EndLine != b.EndLine {
				return a.EndLine < b.EndLine
			}
			return a.EndColumn < b.EndColumn
		})
		// SWR hooks first (row NX-L2): an inline fetcher's own request is
		// the transport of its hook call, like a wrapper's.
		swrRequests := map[string]request{}
		inlineFetches := map[string]bool{}
		for _, c := range calls {
			site := siteKey(file, c.Span)
			if _, ok := requests[site]; ok {
				continue
			}
			if _, found := wrappers[targets[site]]; found {
				continue
			}
			q, inline, swr, ok := r.swrRequest(file, c, wrappers)
			if swr && !ok {
				diagnostics = append(diagnostics, swrUnread(file, c.Span))
			}
			if ok {
				nextTag("NX-L2")
				swrRequests[site] = q
				if inline != "" {
					inlineFetches[inline] = true
				}
			}
		}
		for _, c := range calls {
			q, ok := requests[siteKey(file, c.Span)]
			if !ok {
				if w, found := wrappers[targets[siteKey(file, c.Span)]]; found && r.invokes(file, c, w) {
					q = r.applyWrapper(file, c, w, targets[siteKey(file, c.Span)])
					ok = true
				}
			}
			if !ok {
				q, ok = swrRequests[siteKey(file, c.Span)]
			}
			if !ok {
				continue
			}
			if q.library == "fetch" {
				q.method = r.fetchMethod(file, c.URLScope, q.config)
			}
			method, static := r.literal(file, c.Scope, q.method, nil)
			if static {
				method = strings.ToUpper(method)
			} else {
				method = "DYNAMIC"
			}
			urlText := compact(q.url.Text)
			// Wrapper-derived identities use the actual call-site URL argument text.
			if q.wrapper != "" {
				w := wrappers[q.wrapper]
				for i, p := range w.params {
					if references(w.request.url, p.Name) {
						urlText = compact(arg(c.Expression.Args, i).Text)
						break
					}
				}
			}
			name := method + " " + urlText
			qualified := facts.Join(c.OwnerName, name)
			key := file + "#" + qualified
			duplicates[key]++
			if duplicates[key] > 1 {
				qualified += fmt.Sprintf("@%d", duplicates[key])
			}
			identity := facts.ID("client_call", file, qualified)
			attrs, evaluation := r.pathAttributes(file, c.Scope, c.URLScope, q, method, static, basePaths)
			if q.swr {
				attrs["library"] = "swr"
			}
			if testscope.Classify(file, rules) == testscope.Test {
				attrs["scope"] = "test"
			}
			attrs["url_expression"] = urlText
			if q.library == "hey-api" {
				if security, ok := heySecurity(q); ok {
					attrs["security"] = security
				}
			}
			attrs["wrapper"] = nil
			if q.wrapper != "" {
				attrs["wrapper"] = q.wrapper
			}
			isWrapper := false
			if _, found := wrappers[c.Owner]; found {
				isWrapper = r.wrapperShape(file, c, q, d.Parameters[c.Owner])
			}
			switch {
			case isWrapper || inlineFetches[siteKey(file, c.Span)]:
				attrs["status"] = "wrapper"
			case bundle != nil:
				generated(attrs, bundle)
				bundleCalls++
			default:
				edges = append(edges, matcher.linkPaths(identity, file, c.Span, attrs, evaluation)...)
			}
			nodes = append(nodes, contract.Node{Type: "node", Kind: "client_call", Level: 4, Identity: identity, Name: name, QualifiedName: qualified, File: file, Span: &c.Span, ContentHash: c.Hash, Attributes: attrs})
			edges = append(edges, facts.Edge("contains", c.Owner, identity, file, c.Span))
		}
		if bundleCalls > 0 {
			diagnostics = append(diagnostics, generatedDiagnostic(file, bundle, bundleCalls))
		}
	}
	return nodes, edges, diagnostics
}

// generated records a call of committed bundler output: status generated,
// the marker row, and no candidates; a path entry with a template is
// generated too (one without stays dynamic).
func generated(attrs map[string]any, m *BundleMarker) {
	attrs["status"] = "generated"
	attrs["generated_output"] = m.ID
	paths, _ := attrs["paths"].([]map[string]any)
	for _, p := range paths {
		if p["template"] != nil {
			p["status"] = "generated"
		}
	}
}

func routesProof(files map[string]facts.Facts, modules *typescript.ModuleResolver, nodes []contract.Node) map[string]string {
	return routes.ExpressEquivalence(files, modules, nodes)
}
func siteKey(file string, s contract.Span) string {
	return fmt.Sprintf("%s:%d:%d:%d:%d", file, s.StartLine, s.StartColumn, s.EndLine, s.EndColumn)
}
func references(e expr, name string) bool {
	if e.Kind == "identifier" && e.Name == name {
		return true
	}
	for _, p := range e.Parts {
		if references(p, name) {
			return true
		}
	}
	return false
}

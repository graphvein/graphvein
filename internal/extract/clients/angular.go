package clients

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/testscope"
)

type angularBase struct {
	text, kind string
	opaque     bool
}

func (r *resolver) token(file string, scope int, e expr) typescript.ExportBinding {
	if e.Kind == "identifier" || e.Kind == "type_identifier" {
		b, _, ok := r.binding(file, scope, e.Name)
		if ok && b.Module != "" {
			return r.modules.ImportBinding(file, b.Module, b.Export, "")
		}
		if ok {
			return typescript.ExportBinding{File: file, Name: e.Name}
		}
	}
	if e.Kind == "member_expression" && e.Object != nil {
		b := r.token(file, scope, *e.Object)
		if b.External && b.Name == "*" {
			b.Name = e.Name
			return b
		}
		if b.Namespace {
			return r.modules.Export(b.File, e.Name)
		}
	}
	return typescript.ExportBinding{}
}
func (r *resolver) external(file string, scope int, e expr, module, name string) bool {
	b := r.token(file, scope, e)
	return b.External && !b.Ambiguous && b.File == module && b.Name == name
}
func (r *resolver) injected(file string, c facts.ClientCall) typescript.ExportBinding {
	v := c.Injection
	if v == nil {
		return typescript.ExportBinding{}
	}
	if v.Factory.Kind != "" && !r.external(file, v.Scope, v.Factory, "@angular/core", "inject") && !r.external(file, v.Scope, v.Factory, "@angular/core", "Inject") {
		return typescript.ExportBinding{}
	}
	return r.token(file, v.Scope, v.Token)
}
func (r *resolver) angularRequest(file string, c facts.ClientCall) (request, bool) {
	b := r.injected(file, c)
	if !b.External || b.File != "@angular/common/http" || b.Name != "HttpClient" || b.Ambiguous {
		return request{}, false
	}
	f := c.Expression.Object
	if f == nil {
		return request{}, false
	}
	q := request{library: "angular", url: arg(c.Expression.Args, 0), method: defaultMethod(), angular: r.angularBaseFor(file)}
	switch f.Name {
	case "get", "delete", "head", "options", "post", "put", "patch":
		q.method = expr{Kind: "literal", Name: strings.ToUpper(f.Name)}
	case "jsonp":
	case "request":
		if len(c.Expression.Args) < 2 {
			q.url, q.method = arg(c.Expression.Args, 0), expr{Kind: "dynamic", Text: arg(c.Expression.Args, 0).Text}
		} else {
			q.method, q.url = c.Expression.Args[0], c.Expression.Args[1]
		}
	default:
		return request{}, false
	}
	return q, true
}

func (r *resolver) angularBaseFor(file string) angularBase {
	best, result := -1, angularBase{}
	for root, base := range r.angularBases {
		if (root == "." || file == root || strings.HasPrefix(file, root+"/")) && len(root) > best {
			best, result = len(root), base
		}
	}
	if best < 0 && r.angularTouches {
		return angularBase{kind: "unresolved", opaque: r.angularOpaque}
	}
	return result
}

func visitExpr(e expr, fn func(expr)) {
	fn(e)
	if e.Object != nil {
		visitExpr(*e.Object, fn)
	}
	for _, v := range e.Args {
		visitExpr(v, fn)
	}
	for _, v := range e.Parts {
		visitExpr(v, fn)
	}
	for _, v := range e.Properties {
		visitExpr(v, fn)
	}
}

func (r *resolver) interceptor(file string, scope int, token expr) (string, facts.AngularInterceptor, bool) {
	b := r.token(file, scope, token)
	if b.File == "" || b.External || b.Ambiguous {
		return "", facts.AngularInterceptor{}, false
	}
	d := r.files[b.File].Routes
	if d != nil && d.Angular != nil {
		for _, in := range d.Angular.Interceptors {
			if in.Name == b.Name {
				return b.File, in, true
			}
		}
	}
	return "", facts.AngularInterceptor{}, false
}

// interceptorPrefix accepts only the corpus's single clone, optionally through
// one local const, directly forwarded to next or next.handle.
func (r *resolver) interceptorPrefix(file string, in facts.AngularInterceptor) (expr, bool, bool) {
	touches := false
	for _, e := range in.Expressions {
		if e.Kind == "new_expression" && e.Object != nil && r.external(file, in.Scope, *e.Object, "@angular/common/http", "HttpRequest") {
			touches = true
		}
		if e.Object != nil && e.Object.Kind == "member_expression" && e.Object.Name == "clone" {
			if _, ok := property(arg(e.Args, 0), "url"); ok {
				touches = true
			}
		}
	}
	var returned expr
	locals := map[string]expr{}
	for _, s := range in.Body {
		if s.Kind == "declaration" && s.Constant {
			locals[s.Name] = s.Expression
		} else if s.Kind == "return_statement" {
			returned = s.Expression
		} else {
			return expr{}, touches, false
		}
	}
	if returned.Kind != "call_expression" || returned.Object == nil {
		return expr{}, touches, false
	}
	next := *returned.Object
	forward := next.Kind == "identifier" && next.Name == in.Next || next.Kind == "member_expression" && next.Name == "handle" && next.Object != nil && next.Object.Name == in.Next
	if !forward || len(returned.Args) != 1 {
		return expr{}, touches, false
	}
	clone := returned.Args[0]
	if clone.Kind == "identifier" {
		clone = locals[clone.Name]
	}
	if clone.Kind != "call_expression" || clone.Object == nil || clone.Object.Kind != "member_expression" || clone.Object.Name != "clone" || clone.Object.Object == nil || clone.Object.Object.Name != in.Request {
		// A transformed request forwarded from an opaque callee touches URL.
		if clone.Kind == "call_expression" || clone.Kind == "new_expression" {
			touches = true
		}
		return expr{}, touches, false
	}
	url, ok := property(arg(clone.Args, 0), "url")
	if !ok {
		return expr{}, touches, false
	}
	parts := url.Parts
	if !(url.Kind == "template_string" || url.Kind == "binary_expression" && url.Operator == "+") || len(parts) != 2 {
		return expr{}, true, false
	}
	last := parts[1]
	if last.Kind != "member_expression" || last.Name != "url" || last.Object == nil || last.Object.Name != in.Request {
		return expr{}, true, false
	}
	return parts[0], true, true
}

func (r *resolver) setupAngular(sources map[string][]byte, rules testscope.Rules) []contract.Diagnostic {
	r.angularBases = map[string]angularBase{}
	r.angularTouches, r.angularOpaque = false, false
	var diagnostics []contract.Diagnostic
	for file, source := range sources {
		if path.Base(file) == "angular.json" {
			var config struct {
				Projects map[string]struct {
					SourceRoot string `json:"sourceRoot"`
					Root       string `json:"root"`
				} `json:"projects"`
			}
			if err := json.Unmarshal(source, &config); err != nil {
				diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: "angular_config_parse_error", Message: "parsing angular.json: " + err.Error()})
				continue
			}
			for _, p := range config.Projects {
				root := p.SourceRoot
				if root == "" {
					root = p.Root
				}
				r.angularBases[path.Join(path.Dir(file), root)] = angularBase{}
			}
		}
	}
	rootFor := func(file string) string {
		best := ""
		for root := range r.angularBases {
			if (root == "." || strings.HasPrefix(file, root+"/")) && len(root) > len(best) {
				best = root
			}
		}
		return best
	}
	type registered struct {
		file        string
		in          facts.AngularInterceptor
		conditional bool
	}
	registeredByRoot := map[string]map[string]registered{}
	r.overrides = map[string]bool{}
	r.overrideLocations = map[string][]contract.Evidence{}
	for _, file := range facts.Paths(r.files) {
		d := r.files[file].Routes
		if d == nil || d.Angular == nil || testscope.Classify(file, rules) == testscope.Test {
			continue
		}
		for _, reg := range d.Angular.Registrations {
			root := rootFor(file)
			if registeredByRoot[root] == nil {
				registeredByRoot[root] = map[string]registered{}
			}
			add := func(token expr, conditional bool) {
				key := fmt.Sprintf("%s:%d", file, token.Offset)
				if prior, exists := registeredByRoot[root][key]; exists {
					conditional = conditional || prior.conditional
				}
				f, in, ok := r.interceptor(file, reg.Scope, token)
				if !ok {
					registeredByRoot[root][key] = registered{conditional: true}
					return
				}
				registeredByRoot[root][key] = registered{f, in, conditional}
			}
			// Class providers run only in this provider container's enabled injector.
			enabled := false
			visitExpr(reg.Expression, func(e expr) {
				if r.external(file, reg.Scope, e, "@angular/common/http", "HttpClientModule") || e.Object != nil && r.external(file, reg.Scope, *e.Object, "@angular/common/http", "withInterceptorsFromDi") {
					enabled = true
				}
			})
			visitExpr(reg.Expression, func(e expr) {
				if e.Kind == "object" {
					if p, ok := property(e, "provide"); ok {
						for _, key := range []string{"useClass", "useFactory", "useValue", "useExisting"} {
							if _, ok := property(e, key); ok {
								b := r.token(file, reg.Scope, p)
								r.overrides[b.File+":"+b.Name] = true
								location := contract.Span{StartLine: 1, EndLine: 1}
								if p.Span != nil {
									location = *p.Span
								} else {
									for i, c := range sources[file] {
										if i >= p.Offset {
											break
										}
										if c == '\n' {
											location.StartLine++
											location.StartColumn = 0
										} else {
											location.StartColumn++
										}
									}
									location.EndLine = location.StartLine
									location.EndColumn = location.StartColumn + len(p.Text)
								}
								r.overrideLocations[b.File+":"+b.Name] = append(r.overrideLocations[b.File+":"+b.Name], contract.Evidence{File: file, Span: location})
							}
						}
						if enabled && r.external(file, reg.Scope, p, "@angular/common/http", "HTTP_INTERCEPTORS") && e.Properties["multi"].Kind == "true" {
							add(e.Properties["useClass"], reg.Conditional)
						}
					}
				}
				if e.Object != nil && r.external(file, reg.Scope, *e.Object, "@angular/common/http", "provideHttpClient") {
					for _, feature := range e.Args {
						if feature.Object != nil && r.external(file, reg.Scope, *feature.Object, "@angular/common/http", "withInterceptors") {
							if arg(feature.Args, 0).Kind != "array" {
								add(arg(feature.Args, 0), true)
							}
							for _, token := range arg(feature.Args, 0).Parts {
								add(token, reg.Conditional)
							}
						}
					}
				}
			})
		}
	}
	for root, registrations := range registeredByRoot {
		count := 0
		base := angularBase{}
		opaque := false
		for _, reg := range registrations {
			prefix, touches, proven := r.interceptorPrefix(reg.file, reg.in)
			if reg.file == "" {
				touches = true
			}
			if !touches {
				continue
			}
			r.angularTouches = true
			opaque = opaque || !proven
			r.angularOpaque = r.angularOpaque || !proven
			count++
			base.kind = "unresolved"
			if proven && !reg.conditional {
				ev := newEvaluation(r)
				values := ev.expression(evaluationContext{file: reg.file, scope: reg.in.URLScope}, prefix)
				if len(values) == 1 && values[0].kind == "string" && allLiteral(values[0].pieces) {
					base.text = values[0].text
					base.kind = "literal"
					if prefix.Kind != "literal" {
						base.kind = "constant"
					}
				}
			}
		}
		if count > 1 {
			base = angularBase{kind: "unresolved"}
		}
		base.opaque = opaque
		if root != "" {
			r.angularBases[root] = base
		}
	}
	sort.Slice(diagnostics, func(i, j int) bool { return diagnostics[i].File < diagnostics[j].File })
	return diagnostics
}

// AngularProviderOverrides exposes the existing production override lookup for
// source injection facts without changing receiver or wrapper proof.
func AngularProviderOverrides(files map[string]facts.Facts, modules *typescript.ModuleResolver, sources map[string][]byte, rules testscope.Rules) map[string][]contract.Evidence {
	r := &resolver{files: files, modules: modules, memo: map[string]memoValue{}}
	r.setupAngular(sources, rules)
	return r.overrideLocations
}

func (r *resolver) angularTarget(file string, c facts.ClientCall) string {
	b := r.injected(file, c)
	if b.External || b.Ambiguous || r.overrides[b.File+":"+b.Name] {
		return ""
	}
	d := r.files[b.File].Routes
	if d == nil || d.Angular == nil || c.Expression.Object == nil {
		return ""
	}
	for _, cl := range d.Angular.Classes {
		if cl.Name == b.Name {
			for _, dec := range cl.Decorators {
				if dec.Object != nil && r.external(b.File, 0, *dec.Object, "@angular/core", "Injectable") {
					return cl.Methods[c.Expression.Object.Name]
				}
			}
		}
	}
	return ""
}

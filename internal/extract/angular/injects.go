package angular

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/clients"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

type injectionLinker struct {
	l         *linker
	proof     *linker
	syntax    map[string]typescript.InjectionSyntax
	classes   map[string]bool
	constants map[string]expression
	overrides map[string][]contract.Evidence
	reported  map[string]bool
}

func injectionEligible(d *declaration) bool {
	return d != nil && d.role != "" && d.attrs["conflict"] != true && d.attrs["jit"] != true && d.attrs["metadata"] != "invalid"
}

func (l *linker) injections(syntax map[string]typescript.InjectionSyntax, templates map[string]typescript.TemplateSyntax) {
	r := &injectionLinker{l: l, syntax: syntax, classes: map[string]bool{}, constants: map[string]expression{}, reported: map[string]bool{}}
	r.proof = l
	for file, s := range syntax {
		for _, c := range s.Classes {
			r.classes[c.Identity] = true
		}
		for _, c := range s.Constants {
			r.constants[file+"\x00"+c.Name] = c.Value
		}
	}
	// Avoid running provider analysis for files without a proven Angular source.
	needed := false
	for _, d := range l.classes {
		needed = needed || injectionEligible(d)
	}
	if !needed {
		for file, s := range syntax {
			for _, site := range s.Sites {
				if site.Context == "function" && site.Call.Object != nil && r.core(file, 0, *site.Call.Object, "inject") {
					needed = true
					break
				}
			}
		}
	}
	if !needed {
		return
	}
	proof := *l
	proof.files = map[string]facts.Facts{}
	for file, f := range l.files {
		if f.Routes != nil {
			routes := *f.Routes
			routes.Scopes = append([]facts.RouteScope(nil), routes.Scopes...)
			for _, p := range syntax[file].ParameterScopes {
				if p.Scope < 0 || p.Scope >= len(routes.Scopes) {
					continue
				}
				frame := routes.Scopes[p.Scope]
				frame.Bindings = map[string]facts.RouteBinding{}
				real := map[string]bool{}
				for _, n := range p.Names {
					real[n] = true
				}
				for name, b := range routes.Scopes[p.Scope].Bindings {
					if b.Value.Kind != "parameter" || real[name] {
						frame.Bindings[name] = b
					}
				}
				routes.Scopes[p.Scope] = frame
			}
			f.Routes = &routes
		}
		proof.files[file] = f
	}
	r.proof = &proof
	r.overrides = clients.AngularProviderOverrides(l.files, l.modules, l.sources, l.rules)
	// Injectable own keys are read independently of declaration completeness.
	for _, file := range facts.Paths(l.files) {
		for _, d := range templates[file].Decorators {
			cl := l.classes[d.Class]
			if !injectionEligible(cl) {
				continue
			}
			b := l.target(file, cl.scope, d.Callee)
			if !b.External || b.File != "@angular/core" || b.Name != "Injectable" {
				continue
			}
			for _, raw := range l.syntax[file].Decorators {
				if raw.Offset != d.Callee.Offset || len(raw.Arguments) != 1 || raw.Arguments[0].Kind != "object" {
					continue
				}
				for _, p := range raw.Arguments[0].Entries {
					if p.Kind != "pair" && p.Kind != "shorthand_property_identifier" {
						continue
					}
					if overrideKey(p.Key) {
						n := l.files[file]
						for _, v := range n.Nodes {
							if v.Identity == cl.identity {
								r.overrides[file+":"+v.Name] = append(r.overrides[file+":"+v.Name], contract.Evidence{File: file, Span: d.Span})
							}
						}
					}
				}
			}
		}
	}
	for _, file := range facts.Paths(l.files) {
		s := syntax[file]
		for _, c := range s.Classes {
			d := l.classes[c.Identity]
			if !injectionEligible(d) {
				continue
			}
			counts := map[string]any{}
			for _, key := range []string{"entries", "linked", "external", "unresolved", "invalid", "injection_token", "attribute", "overridden"} {
				counts[key] = 0
			}
			d.attrs["injects"] = counts
			if !c.Constructor && c.Extends {
				counts["inherited"] = true
			}
			for _, p := range c.Parameters {
				r.parameter(d, c, p, counts)
			}
		}
		for _, site := range s.Sites {
			if site.Context == "field" {
				if d := l.classes[site.Source]; d != nil {
					site.Scope = d.scope
				}
			}
			r.proof.shadows = []shadow{{file: file, names: site.Shadows}}
			if site.Call.Object == nil || !r.core(file, site.Scope, *site.Call.Object, "inject") {
				continue
			}
			var counts map[string]any
			if site.Context != "function" {
				d := l.classes[site.Source]
				if !injectionEligible(d) {
					continue
				}
				counts, _ = d.attrs["injects"].(map[string]any)
			}
			token := expression{Kind: "missing", Span: site.Call.Span, Text: "<missing>"}
			if len(site.Call.Args) > 0 {
				token = site.Call.Args[0]
			}
			r.leaf(file, site.Source, site.Context, site.Scope, token, site.Call.Span, counts, "")
		}
		r.proof.shadows = nil
	}
}

func overrideKey(k string) bool {
	return k == "useClass" || k == "useFactory" || k == "useValue" || k == "useExisting"
}

func (r *injectionLinker) core(file string, scope int, e expression, name string) bool {
	b := r.proof.target(file, scope, evaluationExpr(e))
	return b.External && !b.Ambiguous && b.File == "@angular/core" && b.Name == name
}

func (r *injectionLinker) typeOnly(file, name string) bool {
	for _, n := range r.syntax[file].TypeImports {
		if n == name {
			return true
		}
	}
	return false
}

func unwrapInjection(e expression) expression {
	for len(e.Parts) == 1 && (e.Kind == "parenthesized_expression" || e.Kind == "as_expression" || e.Kind == "type_assertion" || e.Kind == "non_null_expression" || e.Kind == "satisfies_expression") {
		e = e.Parts[0]
	}
	return e
}

func injectionEntity(t typescript.InjectionType) (facts.RouteExpr, string) {
	if t.Kind == "type_annotation" && len(t.Children) == 1 {
		return injectionEntity(t.Children[0])
	}
	if t.Kind == "union_type" {
		parts := []typescript.InjectionType{}
		var flatten func(typescript.InjectionType)
		flatten = func(v typescript.InjectionType) {
			if v.Kind == "union_type" {
				for _, c := range v.Children {
					flatten(c)
				}
			} else if v.Text != "null" {
				parts = append(parts, v)
			}
		}
		flatten(t)
		if len(parts) != 1 {
			return facts.RouteExpr{}, "unsupported type"
		}
		return injectionEntity(parts[0])
	}
	if t.Kind == "generic_type" && len(t.Children) > 0 {
		return injectionEntity(t.Children[0])
	}
	if t.Kind == "missing" {
		return facts.RouteExpr{}, "missing type"
	}
	if t.Kind != "type_identifier" && t.Kind != "nested_type_identifier" {
		return facts.RouteExpr{}, "unsupported type"
	}
	parts := strings.Split(t.Text, ".")
	e := facts.RouteExpr{Kind: "identifier", Name: parts[0], Text: parts[0]}
	for _, p := range parts[1:] {
		o := e
		e = facts.RouteExpr{Kind: "member_expression", Name: p, Object: &o, Text: t.Text}
	}
	return e, ""
}

func (r *injectionLinker) parameter(d *declaration, c typescript.InjectionClass, p typescript.InjectionParameter, counts map[string]any) {
	typeText := strings.TrimSpace(strings.TrimPrefix(p.Type.Text, ":"))
	if typeText == "" {
		typeText = "<missing>"
	}
	token := expression{Kind: "missing", Text: typeText, Span: p.Span}
	attribute, decided := false, false
	invalid, unproven := "", false
	for _, dec := range p.Decorators {
		callee := dec
		if dec.Kind == "call_expression" && dec.Object != nil {
			callee = *dec.Object
		}
		e := evaluationExpr(callee)
		head := e
		if e.Kind == "member_expression" && e.Object != nil {
			head = *e.Object
		}
		b, _, ok := r.l.binding(d.file, d.scope, head.Name)
		if !ok || b.Module != "@angular/core" || r.typeOnly(d.file, head.Name) {
			continue
		}
		name := b.Export
		if e.Kind == "member_expression" {
			if b.Export != "*" || head.Kind != "identifier" {
				continue
			}
			name = e.Name
		}
		if !r.core(d.file, d.scope, callee, name) {
			unproven = true
			continue
		}
		switch name {
		case "Inject", "Attribute":
			if dec.Kind != "call_expression" || len(dec.Args) != 1 {
				invalid = "DECORATOR_ARITY_WRONG"
				continue
			}
			token = dec.Args[0]
			attribute = name == "Attribute"
			decided = true
		case "Optional", "Self", "SkipSelf", "Host":
		default:
			invalid = "DECORATOR_UNEXPECTED"
		}
	}
	if invalid != "" {
		r.result(d.file, d.identity, "parameter "+p.Name, typeText, p.Span, counts, "invalid", invalid)
		return
	}
	if unproven {
		r.result(d.file, d.identity, "parameter "+p.Name, typeText, p.Span, counts, "unresolved", "@angular/core is not proven external")
		return
	}
	if attribute {
		r.result(d.file, d.identity, "parameter "+p.Name, token.Text, p.Span, counts, "attribute", "")
		return
	}
	reason := ""
	if !decided {
		v, why := injectionEntity(p.Type)
		reason = why
		token.Value = v
		token.Kind = v.Kind
		token.Text = typeText
		head := v
		for head.Object != nil {
			head = *head.Object
		}
		if r.typeOnly(d.file, head.Name) {
			reason = "type-only import"
		}
		for _, name := range c.TypeParameters {
			if name == head.Name {
				reason = "unknown reference"
			}
		}
		if reason == "" {
			b := r.l.target(d.file, d.scope, v)
			if b.Namespace {
				reason = "namespace alone"
			}
		}
	}
	r.leaf(d.file, d.identity, "parameter "+p.Name, d.scope, token, p.Span, counts, reason)
}

func (r *injectionLinker) token(file string, scope int, e expression, depth int) (typescript.ExportBinding, string, string) {
	if depth > 8 {
		return typescript.ExportBinding{}, "unresolved", "unsupported forwardRef"
	}
	e = unwrapInjection(e)
	if e.Kind == "call_expression" && e.Object != nil && r.core(file, scope, *e.Object, "forwardRef") {
		if len(e.Args) != 1 {
			return typescript.ExportBinding{}, "unresolved", "unsupported forwardRef"
		}
		fn := e.Args[0]
		if (fn.Kind != "arrow_function" && fn.Kind != "function_expression") || len(fn.Parts) != 1 {
			return typescript.ExportBinding{}, "unresolved", "unsupported forwardRef"
		}
		prior := r.proof.shadows
		r.proof.shadows = append(r.proof.shadows, shadow{file: file, names: fn.Names})
		b, out, why := r.token(file, scope, fn.Parts[0], depth+1)
		r.proof.shadows = prior
		return b, out, why
	}
	if e.Kind == "new_expression" && e.Object != nil && r.core(file, scope, *e.Object, "HostAttributeToken") {
		return typescript.ExportBinding{}, "attribute", ""
	}
	if e.Kind != "identifier" && e.Kind != "member_expression" {
		return typescript.ExportBinding{}, "unresolved", "unsupported token expression: " + e.Kind
	}
	v := evaluationExpr(e)
	head := v
	for head.Object != nil {
		head = *head.Object
	}
	for _, frame := range r.proof.shadows {
		if frame.file == file {
			for _, name := range frame.names {
				if name == head.Name {
					return typescript.ExportBinding{}, "unresolved", "locally shadowed"
				}
			}
		}
	}
	if r.typeOnly(file, head.Name) {
		return typescript.ExportBinding{}, "unresolved", "type-only import"
	}
	b := r.proof.target(file, scope, v)
	if b.Ambiguous || b.Namespace {
		return b, "unresolved", "unknown reference"
	}
	if b.External {
		return b, "external", ""
	}
	if r.classes[b.Identity] {
		return b, "linked", ""
	}
	if value, ok := r.constants[b.File+"\x00"+b.Name]; ok {
		value = unwrapInjection(value)
		if value.Kind == "new_expression" && value.Object != nil && r.core(b.File, 0, *value.Object, "InjectionToken") {
			return b, "injection_token", ""
		}
	}
	if b.File != "" && b.Name != "" {
		return b, "unresolved", "target is not a class declaration"
	}
	return b, "unresolved", "unknown reference"
}

func (r *injectionLinker) leaf(file, source, site string, scope int, e expression, span contract.Span, counts map[string]any, reason string) {
	if reason != "" {
		r.result(file, source, site, e.Text, span, counts, "unresolved", reason)
		return
	}
	b, out, why := r.token(file, scope, e, 0)
	if out == "attribute" && strings.HasPrefix(site, "parameter ") {
		out, why = "unresolved", "unsupported token expression: new_expression"
	}
	target := b.Identity
	if out == "external" {
		target = r.l.external(file, b.File)
		if target == "" {
			out = "unresolved"
			why = "external import node is unavailable"
		}
	}
	r.result(file, source, site, e.Text, span, counts, out, why)
	if out != "linked" && out != "external" {
		return
	}
	r.l.edges = append(r.l.edges, facts.Edge("injects", source, target, file, span))
	locations, overridden := r.overrides[b.File+":"+b.Name]
	if !overridden {
		return
	}
	if counts != nil {
		count(counts, "overridden")
	}
	key := source + "\x00" + b.File + "\x00" + b.Name
	if r.reported[key] {
		return
	}
	r.reported[key] = true
	names := []string{}
	seen := map[string]bool{}
	for _, loc := range locations {
		n := fmt.Sprintf("%s:%d", loc.File, loc.Span.StartLine)
		if !seen[n] {
			names = append(names, n)
			seen[n] = true
		}
	}
	sort.Strings(names)
	r.l.diagnostics = append(r.l.diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "info", Code: "angular_injection_overridden", Message: fmt.Sprintf("%s %s token %s requested %s; provider at %s", source, site, e.Text, target, strings.Join(names, ", "))})
}

func (r *injectionLinker) result(file, source, site, token string, span contract.Span, counts map[string]any, outcome, reason string) {
	if counts != nil {
		count(counts, "entries")
		count(counts, outcome)
	}
	if outcome != "invalid" && outcome != "unresolved" {
		return
	}
	severity := "info"
	if outcome == "invalid" {
		severity = "warning"
	}
	r.l.diagnostics = append(r.l.diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Code: "angular_injection_" + outcome, Severity: severity, Message: fmt.Sprintf("%s %s at %d:%d token %s: %s", source, site, span.StartLine, span.StartColumn, token, reason)})
}

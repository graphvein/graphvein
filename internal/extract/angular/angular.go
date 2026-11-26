// Package angular proves declaration metadata against the current source inventory.
package angular

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/clients"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/template"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/testscope"
)

type expression = typescript.DeclarationExpr
type declaration struct {
	file, identity, role string
	span                 contract.Span
	scope, urlScope      int
	metadata             map[string]expression
	attrs                map[string]any
	inline               []typescript.InlineTemplate
	active               bool
	orderedImports       []string
	// orderedExports holds local export leaves; the router closure follows
	// them because an NgModule injector also imports its exports.
	orderedExports []string
}

type linker struct {
	files           map[string]facts.Facts
	modules         *typescript.ModuleResolver
	syntax          map[string]typescript.DeclarationSyntax
	templates       map[string]template.Facts
	sources         map[string][]byte
	root            string
	rules           testscope.Rules
	projects        []project
	versions        map[string]versionInfo
	versionReported map[string]bool
	classes         map[string]*declaration
	constants       map[string]expression
	mutated         map[string]bool
	externals       map[string]string
	edges           []contract.Edge
	evidence        []contract.Evidence
	shadows         []shadow
	diagnostics     []contract.Diagnostic
	routeTuples     []routeTuple
	prepared        bool
}

type shadow struct {
	file  string
	names []string
}

// Link returns class overlays and link-time ui_route nodes, leaving source facts
// and existing graph records intact. Routing facts are derived from cached
// router syntax on every relink, so edits to an imported routes file relink
// other files' routes without reparsing them.
func Link(files map[string]facts.Facts, modules *typescript.ModuleResolver, templatesSyntax map[string]typescript.TemplateSyntax, syntax map[string]typescript.DeclarationSyntax, templates map[string]template.Facts, sources map[string][]byte, importEdges []contract.Edge, root string, rules testscope.Rules, injectionSyntax map[string]typescript.InjectionSyntax, routers map[string]typescript.RouterSyntax) (map[string]map[string]any, []contract.Node, []contract.Edge, []contract.Diagnostic) {
	l := &linker{files: files, modules: modules, syntax: syntax, templates: templates, sources: sources, root: root, rules: rules, versions: map[string]versionInfo{}, versionReported: map[string]bool{}, classes: map[string]*declaration{}, constants: map[string]expression{}, mutated: map[string]bool{}, externals: map[string]string{}}
	for _, file := range facts.Paths(files) {
		l.capture(file, templatesSyntax[file], syntax[file])
	}
	routing := false
	for _, r := range routers {
		routing = routing || len(r.Calls) > 0
	}
	if len(l.classes) == 0 && len(injectionSyntax) == 0 && !routing {
		return map[string]map[string]any{}, nil, nil, l.diagnostics
	}
	l.projects = projects(sources)
	for _, edge := range importEdges {
		if edge.Kind == "imports" && strings.HasPrefix(edge.Target, "external:") {
			for _, ev := range edge.Evidence {
				l.externals[ev.File+"\x00"+edge.Target] = edge.Target
				l.externals[edge.Target] = edge.Target
			}
		}
	}
	ids := []string{}
	for id := range l.classes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		l.values(l.classes[id])
	}
	if len(ids) > 0 {
		l.prepareConstants()
		for _, id := range ids {
			l.scopes(l.classes[id])
		}
		l.duplicates()
	}
	if len(injectionSyntax) > 0 {
		l.injections(injectionSyntax, templatesSyntax)
	}
	l.matchTemplates(ids)
	var routeNodes []contract.Node
	if routing {
		if !l.prepared {
			l.prepareConstants()
		}
		routeNodes = l.routes(routers, injectionSyntax, templatesSyntax)
	}
	attrs := map[string]map[string]any{}
	for id, d := range l.classes {
		attrs[id] = d.attrs
	}
	_, edges := facts.Finish(nil, l.edges)
	for i := range edges {
		seen := map[contract.Evidence]bool{}
		unique := []contract.Evidence{}
		for _, ev := range edges[i].Evidence {
			if !seen[ev] {
				seen[ev] = true
				unique = append(unique, ev)
			}
		}
		sort.Slice(unique, func(i, j int) bool {
			a, b := unique[i], unique[j]
			if a.File != b.File {
				return a.File < b.File
			}
			if a.Span.StartLine != b.Span.StartLine {
				return a.Span.StartLine < b.Span.StartLine
			}
			if a.Span.StartColumn != b.Span.StartColumn {
				return a.Span.StartColumn < b.Span.StartColumn
			}
			if a.Span.EndLine != b.Span.EndLine {
				return a.Span.EndLine < b.Span.EndLine
			}
			return a.Span.EndColumn < b.Span.EndColumn
		})
		edges[i].Evidence = unique
	}
	return attrs, routeNodes, edges, l.diagnostics
}

func (l *linker) diagnostic(d *declaration, key, code, severity, reason string) {
	l.diagnostics = append(l.diagnostics, contract.Diagnostic{Type: "diagnostic", File: d.file, Code: code, Severity: severity, Message: fmt.Sprintf("%s metadata %s: %s", d.identity, key, reason)})
}

func (l *linker) binding(file string, scope int, name string) (facts.RouteBinding, int, bool) {
	r := l.files[file].Routes
	if r == nil {
		return facts.RouteBinding{}, 0, false
	}
	for at := scope; at >= 0 && at < len(r.Scopes); at = r.Scopes[at].Parent {
		if b, ok := r.Scopes[at].Bindings[name]; ok {
			return b, at, true
		}
	}
	return facts.RouteBinding{}, 0, false
}

func (l *linker) target(file string, scope int, e facts.RouteExpr) typescript.ExportBinding {
	if e.Kind == "member_expression" && e.Object != nil {
		return l.modules.Member(l.target(file, scope, *e.Object), e.Name)
	}
	if e.Kind != "identifier" {
		return typescript.ExportBinding{}
	}
	for _, frame := range l.shadows {
		if frame.file == file {
			for _, name := range frame.names {
				if name == e.Name {
					return typescript.ExportBinding{}
				}
			}
		}
	}
	b, at, ok := l.binding(file, scope, e.Name)
	if !ok || b.Value.Invalid {
		return typescript.ExportBinding{}
	}
	if b.Module != "" {
		return l.modules.ImportBinding(file, b.Module, b.Export, "")
	}
	if b.Value.Kind == "static_class" {
		return typescript.ExportBinding{File: file, Name: e.Name, Identity: b.Value.Name}
	}
	if at == 0 {
		return typescript.ExportBinding{File: file, Name: e.Name}
	}
	return typescript.ExportBinding{}
}

var roles = map[string]string{"Component": "component", "Directive": "directive", "Pipe": "pipe", "NgModule": "ng_module", "Injectable": "injectable"}

func (l *linker) capture(file string, syntax typescript.TemplateSyntax, raw typescript.DeclarationSyntax) {
	for i, cached := range syntax.Decorators {
		var original *typescript.DeclarationDecorator
		for j := range raw.Decorators {
			if raw.Decorators[j].Offset == cached.Callee.Offset {
				original = &raw.Decorators[j]
				break
			}
		}
		if original == nil {
			continue
		}
		d := &declaration{file: file, identity: cached.Class, span: cached.Span, scope: original.Scope, urlScope: original.URLScope, attrs: map[string]any{}, metadata: map[string]expression{}}
		if cached.Class == "" {
			d.identity = fmt.Sprintf("unidentified class at %s:%d", file, cached.Span.StartLine)
		}
		head := cached.Callee
		if head.Kind == "member_expression" && head.Object != nil {
			head = *head.Object
		}
		b, _, ok := l.binding(file, d.scope, head.Name)
		name := b.Export
		if cached.Callee.Kind == "member_expression" {
			name = cached.Callee.Name
		}
		resolved := l.target(file, d.scope, cached.Callee)
		classProven := false
		for _, n := range l.files[file].Nodes {
			if n.Kind == "class" && n.Identity == cached.Class {
				classProven = true
				break
			}
		}
		direct := ok && b.Module == "@angular/core" && !b.Value.Invalid && (cached.Callee.Kind == "identifier" || cached.Callee.Kind == "member_expression" && head.Kind == "identifier" && b.Export == "*")
		proven := direct && resolved.External && resolved.File == "@angular/core" && resolved.Name == name && roles[name] != ""
		if !classProven {
			l.diagnostic(d, "decorator", "angular_decorator_unproven", "info", "class identity is unavailable")
			continue
		}
		if !proven {
			if resolved.External && resolved.File == "@angular/core" && roles[resolved.Name] != "" && !direct {
				l.diagnostic(d, "decorator", "angular_decorator_indirect", "info", "Angular decorator imported indirectly; no role assigned")
			} else if roles[name] != "" && b.Module == "@angular/core" {
				l.diagnostic(d, "decorator", "angular_decorator_unproven", "info", "Angular decorator or class identity is unproven")
			}
			continue
		}
		d.role = roles[name]
		invalidInjectable := false
		previous := l.classes[cached.Class]
		if previous != nil {
			if previous.attrs["conflict"] == true {
				continue
			}
			if d.role == "injectable" {
				if previous.role != "injectable" {
					previous.attrs["injectable"] = true
				}
				if len(original.Arguments) > 1 || len(original.Arguments) == 1 && original.Arguments[0].Kind != "object" {
					previous.active = false
					previous.attrs["metadata"] = "invalid"
					reason := "Injectable expects zero or one argument"
					if len(original.Arguments) == 1 {
						reason = "Injectable argument must be an object literal"
					}
					l.diagnostic(previous, "decorator", "angular_decorator_invalid", "warning", reason)
				}
				continue
			}
			if previous.role != "injectable" {
				previous.role = ""
				previous.active = false
				previous.attrs = map[string]any{"role": nil, "conflict": true}
				l.diagnostic(previous, "decorator", "angular_decorator_conflict", "warning", "multiple primary decorators")
				continue
			}
			d.attrs["injectable"] = true
			invalidInjectable = previous.attrs["metadata"] == "invalid"
		}
		d.attrs["role"] = d.role
		d.attrs["metadata"] = "complete"
		valid := len(original.Arguments) <= 1
		if d.role == "injectable" && len(original.Arguments) == 1 {
			valid = original.Arguments[0].Kind == "object"
		}
		if d.role != "injectable" && len(original.Arguments) == 1 {
			arg := unwrap(original.Arguments[0])
			valid = arg.Kind == "object"
			if valid {
				for _, entry := range arg.Entries {
					if entry.Kind != "pair" && entry.Kind != "shorthand_property_identifier" {
						d.attrs["metadata"] = "partial"
					}
					d.metadata[entry.Key] = entry.Value
				}
			}
		}
		if !valid {
			d.attrs["metadata"] = "invalid"
			reason := "expected zero arguments or one object literal (parentheses/as only)"
			if d.role == "injectable" && len(original.Arguments) == 1 {
				reason = "Injectable argument must be an object literal"
			}
			l.diagnostic(d, "decorator", "angular_decorator_invalid", "warning", reason)
		} else if d.attrs["metadata"] == "partial" {
			l.diagnostic(d, "decorator", "angular_metadata_partial", "info", "spread, computed key, method or accessor prevents metadata evaluation")
		} else {
			d.active = true
		}
		if d.role == "pipe" && len(original.Arguments) == 1 && d.active {
			if _, ok := d.metadata["name"]; !ok {
				d.active = false
				d.attrs["metadata"] = "invalid"
				l.diagnostic(d, "name", "angular_decorator_invalid", "warning", "pipe metadata requires a name")
			}
		}
		if invalidInjectable {
			d.attrs["metadata"] = "invalid"
			d.active = false
		}
		for _, in := range syntax.Inline {
			if in.Decorator == i {
				d.inline = append(d.inline, in)
			}
		}
		l.classes[d.identity] = d
	}
}

func unwrap(e expression) expression {
	for (e.Kind == "parenthesized_expression" || e.Kind == "as_expression") && len(e.Parts) == 1 {
		e = e.Parts[0]
	}
	return e
}

func (l *linker) value(d *declaration, key, kind string) (any, bool) {
	e, exists := d.metadata[key]
	if !exists {
		return nil, false
	}
	v := clients.EvaluateMetadata(l.files, l.modules, d.file, d.urlScope, evaluationExpr(e))
	if !v.Known {
		unknownKey(d, key)
		l.diagnostic(d, key, "angular_value_unevaluated", "info", "value cannot be evaluated")
		return nil, true
	}
	if v.Kind != kind {
		unknownKey(d, key)
		l.diagnostic(d, key, "angular_value_wrong_type", "warning", "expected "+kind)
		return nil, true
	}
	if kind == "boolean" {
		return v.Text == "true", true
	}
	return v.Text, true
}

func unknownKey(d *declaration, key string) {
	keys, _ := d.attrs["unknown_keys"].([]string)
	for _, existing := range keys {
		if existing == key {
			return
		}
	}
	keys = append(keys, key)
	sort.Strings(keys)
	d.attrs["unknown_keys"] = keys
}

func keyUnknown(d *declaration, key string) bool {
	keys, _ := d.attrs["unknown_keys"].([]string)
	for _, existing := range keys {
		if existing == key {
			return true
		}
	}
	return false
}

func evaluationExpr(e expression) facts.RouteExpr {
	// Value expressions use the unchanged evaluator, which treats assertion
	// wrappers transparently. Only the metadata object has Angular's strict unwrap.
	for len(e.Parts) == 1 && (e.Kind == "parenthesized_expression" || e.Kind == "as_expression" || e.Kind == "satisfies_expression" || e.Kind == "type_assertion" || e.Kind == "non_null_expression") {
		e = e.Parts[0]
	}
	if e.Kind == "object" {
		value := facts.RouteExpr{Kind: "object", Properties: map[string]facts.RouteExpr{}, Span: &e.Span}
		for _, entry := range e.Entries {
			if entry.Kind != "pair" && entry.Kind != "shorthand_property_identifier" {
				value.Invalid = true
				continue
			}
			if _, exists := value.Properties[entry.Key]; !exists {
				value.Keys = append(value.Keys, entry.Key)
			}
			value.Properties[entry.Key] = evaluationExpr(entry.Value)
		}
		return value
	}
	return e.Value
}

func (l *linker) values(d *declaration) {
	if d.attrs["conflict"] == true {
		return
	}
	if d.role == "component" || d.role == "directive" {
		d.attrs["selector"] = nil
	}
	if d.role == "component" || d.role == "directive" || d.role == "pipe" {
		d.attrs["standalone"] = nil
		d.attrs["standalone_source"] = "unknown"
	}
	if d.role == "pipe" {
		d.attrs["name"] = nil
		d.attrs["pure"] = true
	}
	if !d.active {
		return
	}
	if _, jit := d.metadata["jit"]; jit && (d.role == "component" || d.role == "directive" || d.role == "ng_module") {
		d.attrs["jit"] = true
		d.active = false
		return
	}
	if d.role == "component" || d.role == "directive" {
		if value, _ := l.value(d, "selector", "string"); value != "" {
			d.attrs["selector"] = value
		}
		if value, present := l.value(d, "exportAs", "string"); present {
			if text, ok := value.(string); ok {
				values := []string{}
				for _, v := range strings.Split(text, ",") {
					values = append(values, strings.TrimSpace(v))
				}
				d.attrs["export_as"] = values
			} else {
				d.attrs["export_as"] = nil
			}
		}
	}
	if d.role == "component" || d.role == "directive" || d.role == "pipe" {
		value, present := l.value(d, "standalone", "boolean")
		if value != nil {
			d.attrs["standalone"] = value
			d.attrs["standalone_source"] = "explicit"
		} else if !present {
			v := l.version(d.file)
			if v.major >= 0 {
				d.attrs["standalone"] = v.major >= 19
				d.attrs["standalone_source"] = "default"
			} else if !l.versionReported[v.key] {
				l.versionReported[v.key] = true
				l.diagnostic(d, "standalone", "angular_version_unknown", "info", "core/compiler-cli majors unavailable or inconsistent for "+v.key)
			}
		}
	}
	if d.role == "pipe" {
		if value, present := l.value(d, "name", "string"); present {
			d.attrs["name"] = value
		}
		if value, present := l.value(d, "pure", "boolean"); present {
			d.attrs["pure"] = value
		}
	}
	if d.role == "component" {
		l.componentTemplate(d)
	}
}

func packageName(module string) string {
	parts := strings.Split(module, "/")
	if strings.HasPrefix(module, "@") && len(parts) > 1 {
		return strings.Join(parts[:2], "/")
	}
	return parts[0]
}

func (l *linker) external(file, module string) string {
	id := facts.ID("external", ".", "npm:"+packageName(module))
	if target := l.externals[file+"\x00"+id]; target != "" {
		return target
	}
	return l.externals[id]
}

func (l *linker) componentTemplate(d *declaration) {
	if value, exists := l.value(d, "templateUrl", "string"); exists {
		t := map[string]any{"kind": "external", "status": "unresolved"}
		d.attrs["template"] = t
		url, ok := value.(string)
		if !ok {
			return
		}
		t["path"] = url
		p := path.Clean(path.Join(path.Dir(d.file), url))
		if strings.HasPrefix(url, "/") || p == ".." || strings.HasPrefix(p, "../") || strings.Contains(url, "\\") {
			l.diagnostic(d, "templateUrl", "angular_template_unresolved", "info", "resource path is rooted or outside repository")
			return
		}
		if strings.HasPrefix(url, ".") && !strings.HasPrefix(url, "./") && !strings.HasPrefix(url, "../") {
			l.diagnostic(d, "templateUrl", "angular_template_unresolved", "info", "resource path is not relative")
			return
		}
		t["path"] = p
		if _, ok := l.sources[p]; !ok {
			t["status"] = "missing"
			message := "template file is absent: " + p
			if l.root != "" {
				if _, err := os.Stat(filepath.Join(l.root, filepath.FromSlash(p))); err == nil {
					message = "template file exists but is not scanned: " + p
					if source := clients.ExclusionSource(p, l.sources); source != "" {
						message = "template file is excluded by " + source + ": " + p
					}
				}
			}
			l.diagnostic(d, "templateUrl", "angular_template_missing", "warning", message)
			return
		}
		t["status"] = "unparsed"
		if facts, ok := l.templates[p]; ok && facts.Valid() {
			t["status"] = "linked"
		}
		l.edges = append(l.edges, facts.Edge("uses_template", d.identity, facts.ID("file", p, p), d.file, d.metadata["templateUrl"].Span))
		return
	}
	if _, exists := d.metadata["template"]; !exists {
		l.diagnostic(d, "template", "angular_template_absent", "warning", "component has neither templateUrl nor template")
		return
	}
	status := "nonliteral"
	last := d.metadata["template"]
	for _, in := range d.inline {
		if in.Span == last.Span {
			status = in.Reason
			if status == "" && in.Facts != nil && in.Facts.Valid() {
				status = "parsed"
			}
		}
	}
	d.attrs["template"] = map[string]any{"kind": "inline", "status": status}
	if status != "parsed" {
		severity := "info"
		if status == "parse_failed" || status == "extract_failed" {
			severity = "warning"
		}
		l.diagnostic(d, "template", "angular_template_inline_unparsed", severity, status)
	}
}

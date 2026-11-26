package angular

import (
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/testscope"
)

func (l *linker) prepareConstants() {
	l.prepared = true
	for _, file := range facts.Paths(l.files) {
		for _, c := range l.syntax[file].Constants {
			l.constants[file+"\x00"+c.Name] = c.Value
		}
	}
	for _, file := range facts.Paths(l.files) {
		for _, m := range l.syntax[file].Mutations {
			e := m.Value
			b := l.mutationTarget(file, m.Scope, e)
			if b.File != "" && !b.External {
				if b.Namespace && e.Kind != "identifier" {
					// A dynamic namespace write can affect any exported array.
					for _, f := range l.files {
						for _, c := range f.ExportClauses {
							if c.Alias == "" {
								continue
							}
							target := l.modules.Member(b, c.Alias)
							l.mutated[target.File+"\x00"+target.Name] = true
						}
						for name := range f.Exports {
							target := l.modules.Member(b, name)
							l.mutated[target.File+"\x00"+target.Name] = true
						}
						if f.Routes != nil {
							for name := range f.Routes.Exports {
								target := l.modules.Member(b, name)
								l.mutated[target.File+"\x00"+target.Name] = true
							}
						}
					}
				}
				l.mutated[b.File+"\x00"+b.Name] = true
			}
		}
	}
}

func (l *linker) mutationTarget(file string, scope int, e facts.RouteExpr) typescript.ExportBinding {
	if e.Kind == "subscript_expression" && e.Object != nil {
		base := l.mutationTarget(file, scope, *e.Object)
		if base.Namespace && len(e.Parts) == 1 && e.Parts[0].Kind == "literal" {
			return l.modules.Member(base, e.Parts[0].Name)
		}
		return base
	}
	if e.Kind == "member_expression" && e.Object != nil {
		base := l.mutationTarget(file, scope, *e.Object)
		if base.Namespace {
			return l.modules.Member(base, e.Name)
		}
		return base
	}
	if e.Kind == "identifier" {
		b, at, ok := l.binding(file, scope, e.Name)
		if !ok || at == 0 && b.Module == "" && b.Value.Kind == "" {
			// Ordinary route extraction erases reassigned identifier bindings.
			// Original imports still identify cross-file array mutation targets.
			for _, imp := range l.files[file].Imports {
				if name, found := imp.Bindings[e.Name]; found {
					return l.modules.ImportBinding(file, imp.Module, name, "")
				}
			}
		}
	}
	return l.target(file, scope, e)
}

func (l *linker) scopes(d *declaration) {
	if !d.active {
		return
	}
	keys := []string{}
	if d.role == "ng_module" {
		keys = []string{"declarations", "imports", "exports"}
	}
	if d.role == "component" {
		keys = []string{"imports"}
	}
	for _, key := range keys {
		counts := map[string]any{"entries": 0, "linked": 0, "external": 0, "unresolved": 0, "invalid": 0}
		d.attrs[key] = counts
		e, exists := d.metadata[key]
		if !exists {
			continue
		}
		blocked := d.role == "component" && d.attrs["standalone"] != true
		if blocked && d.attrs["standalone"] == false {
			l.diagnostic(d, key, "angular_imports_not_standalone", "warning", "imports require a standalone component")
		}
		if blocked && d.attrs["standalone"] == nil {
			l.diagnostic(d, key, "angular_scope_entry_unresolved", "info", "component standalone value cannot be proven")
		}
		l.array(d, key, e, d.file, d.scope, counts, map[string]bool{}, true, blocked)
	}
}

func count(m map[string]any, key string) { m[key] = m[key].(int) + 1 }

func (l *linker) unresolved(d *declaration, key string, counts map[string]any, reason string) {
	count(counts, "entries")
	count(counts, "unresolved")
	l.diagnostic(d, key, "angular_scope_entry_unresolved", "info", reason)
}

func (l *linker) array(d *declaration, key string, e expression, file string, scope int, counts map[string]any, seen map[string]bool, whole, blocked bool) {
	before := len(l.evidence)
	l.evidence = append(l.evidence, contract.Evidence{File: file, Span: e.Span})
	defer func() { l.evidence = l.evidence[:before] }()
	e = unwrap(e)
	if e.Kind == "array" {
		for _, p := range e.Parts {
			l.array(d, key, p, file, scope, counts, seen, false, blocked)
		}
		return
	}
	if e.Kind == "spread_element" && len(e.Parts) == 1 {
		p := unwrap(e.Parts[0])
		if p.Kind == "array" {
			l.array(d, key, p, file, scope, counts, seen, true, blocked)
			return
		}
		if l.constantArray(d, key, p, file, scope, counts, seen, blocked) {
			return
		}
		l.unresolved(d, key, counts, "spread is not a proven const array")
		return
	}
	if l.constantArray(d, key, e, file, scope, counts, seen, blocked) {
		return
	}
	if whole {
		l.unresolved(d, key, counts, "whole value is not an array or proven const array")
		return
	}
	if e.Kind == "call_expression" && e.Object != nil {
		b := l.target(file, scope, evaluationExpr(*e.Object))
		if b.External && b.File == "@angular/core" && b.Name == "forwardRef" {
			var fn expression
			if len(e.Args) == 1 {
				fn = unwrap(e.Args[0])
			}
			if len(e.Args) == 1 && (fn.Kind == "arrow_function" || fn.Kind == "function_expression") && len(fn.Parts) == 1 && fn.Name == "" {
				l.shadows = append(l.shadows, shadow{file: file, names: fn.Names})
				l.array(d, key, fn.Parts[0], file, scope, counts, seen, false, blocked)
				l.shadows = l.shadows[:len(l.shadows)-1]
				return
			}
			l.unresolved(d, key, counts, "forwardRef requires one function with one return expression")
			return
		}
		if key != "declarations" && d.role == "ng_module" && b.External {
			if key == "imports" {
				// Transient owner evidence for router registrations; never cached.
				l.routeTuples = append(l.routeTuples, routeTuple{module: d.identity, file: file, span: e.Span})
			}
			l.leaf(d, key, file, e.Span, b.Identity, l.external(file, b.File), counts, blocked)
			return
		}
		l.unresolved(d, key, counts, "call is not a supported external ModuleWithProviders or forwardRef")
		return
	}
	if e.Kind == "object" && key != "declarations" {
		var target *expression
		valid := true
		for i := range e.Entries {
			p := &e.Entries[i]
			if p.Kind == "spread_element" || p.Kind == "computed" {
				valid = false
			}
			if p.Key == "ngModule" {
				target = &p.Value
			}
		}
		if valid && target != nil {
			l.array(d, key, *target, file, scope, counts, seen, false, blocked)
			return
		}
		l.unresolved(d, key, counts, "object has no proven own ngModule property")
		return
	}
	if e.Kind == "identifier" || e.Kind == "member_expression" {
		b := l.target(file, scope, evaluationExpr(e))
		if b.External && !b.Ambiguous {
			l.leaf(d, key, file, e.Span, "", l.external(file, b.File), counts, blocked)
			return
		}
		if b.Identity != "" {
			l.leaf(d, key, file, e.Span, b.Identity, "", counts, blocked)
			return
		}
		if b.File != "" && b.Name != "" && !b.Namespace && !b.Ambiguous {
			l.unresolved(d, key, counts, "target is not a class declaration")
			return
		}
	}
	l.unresolved(d, key, counts, "unsupported or unresolved entry: "+e.Kind)
}

func (l *linker) constantArray(d *declaration, key string, e expression, file string, scope int, counts map[string]any, seen map[string]bool, blocked bool) bool {
	if e.Kind != "identifier" && e.Kind != "member_expression" {
		return false
	}
	b := l.target(file, scope, evaluationExpr(e))
	id := b.File + "\x00" + b.Name
	value, ok := l.constants[id]
	if !ok {
		return false
	}
	if l.mutated[id] || seen[id] {
		l.unresolved(d, key, counts, "mutated or cyclic const array")
		return true
	}
	seen[id] = true
	shadows := l.shadows
	l.shadows = nil
	l.array(d, key, value, b.File, 0, counts, seen, true, blocked)
	l.shadows = shadows
	delete(seen, id)
	return true
}

func (l *linker) leaf(d *declaration, key, file string, span contract.Span, target, external string, counts map[string]any, blocked bool) {
	count(counts, "entries")
	invalid, unknown := false, false
	reason := "target role or standalone value cannot be proven: " + target
	if external != "" {
		unknown = key == "declarations"
		if unknown {
			reason = "target is not a class declaration"
		}
	} else if target != "" && !strings.HasPrefix(target, "class:") {
		unknown = true
		reason = "target is not a class declaration"
	} else if t := l.classes[target]; t != nil {
		switch t.role {
		case "ng_module":
			invalid = key == "declarations"
		case "component", "directive", "pipe":
			standalone := t.attrs["standalone"]
			unknown = standalone == nil
			if key == "declarations" {
				invalid = standalone == true || t.role == "directive" && t.attrs["selector"] == nil
				if t.role == "directive" && keyUnknown(t, "selector") {
					unknown, invalid = true, false
				}
			}
			if key == "imports" {
				invalid = standalone == false
			}
		default:
			unknown = t.attrs["conflict"] == true
			invalid = !unknown
		}
	} else if target != "" {
		invalid = true
	} else {
		unknown = true
	}
	if blocked {
		if d.attrs["standalone"] == false {
			count(counts, "invalid")
		} else {
			count(counts, "unresolved")
		}
		return
	}
	if invalid {
		count(counts, "invalid")
		l.diagnostic(d, key, "angular_scope_entry_invalid", "warning", "invalid target "+target+external)
		return
	}
	if unknown {
		count(counts, "unresolved")
		l.diagnostic(d, key, "angular_scope_entry_unresolved", "info", reason)
		return
	}
	if external != "" {
		target = external
		count(counts, "external")
	} else {
		count(counts, "linked")
	}
	// Capture leaves before Finish merges edges: external classes can share a
	// package target, and repeated local classes must retain their first position.
	if key == "imports" {
		d.orderedImports = append(d.orderedImports, target)
	}
	if key == "exports" && external == "" {
		d.orderedExports = append(d.orderedExports, target)
	}
	kind := map[string]string{"declarations": "declares", "imports": "scope_imports", "exports": "scope_exports"}[key]
	edge := facts.Edge(kind, d.identity, target, file, span)
	edge.Evidence = append(edge.Evidence, l.evidence...)
	l.edges = append(l.edges, edge)
}

func (l *linker) duplicates() {
	groups := map[string]map[string]bool{}
	for _, e := range l.edges {
		if e.Kind != "declares" {
			continue
		}
		d := l.classes[e.Source]
		key := e.Target + "\x00" + string(testscope.Classify(d.file, l.rules))
		if groups[key] == nil {
			groups[key] = map[string]bool{}
		}
		groups[key][e.Source] = true
	}
	blocked := map[string]bool{}
	keys := []string{}
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		modules := groups[key]
		if len(modules) < 2 {
			continue
		}
		names := []string{}
		for name := range modules {
			names = append(names, name)
		}
		sort.Strings(names)
		target, _, _ := strings.Cut(key, "\x00")
		l.diagnostic(l.classes[target], "declarations", "angular_duplicate_declaration", "warning", "declared by "+strings.Join(names, ", "))
		for module := range modules {
			blocked[module+"\x00"+target] = true
		}
	}
	kept := l.edges[:0]
	for _, e := range l.edges {
		if e.Kind == "declares" && blocked[e.Source+"\x00"+e.Target] {
			counts := l.classes[e.Source].attrs["declarations"].(map[string]any)
			counts["linked"] = counts["linked"].(int) - 1
			count(counts, "invalid")
			continue
		}
		kept = append(kept, e)
	}
	l.edges = kept
}

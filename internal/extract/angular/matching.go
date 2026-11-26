package angular

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/template"
)

type matchingCandidate struct {
	d                          *declaration
	selectors                  []selectorCompound
	unmodeled, unknown, absent bool
}

type matchingAttribute struct {
	value          string
	span           contract.Span
	valueSpan      contract.Span
	uncertainValue bool
}

type matchingNode struct {
	name       string
	span       contract.Span
	nameSpan   contract.Span
	attrs      map[string]matchingAttribute
	uncertain  bool
	structural bool
	secondary  string
}

func matchingCounts(pipe bool) map[string]any {
	m := map[string]any{"entries": 0, "linked": 0, "external_scope": 0, "unknown": 0, "unmatched": 0, "invalid": 0}
	if pipe {
		m["shadowable"] = 0
	}
	return m
}

func addMatchingCount(m map[string]any, status string) {
	count(m, "entries")
	count(m, status)
}

func (l *linker) matchTemplates(ids []string) {
	b := newScopeBuilder(l)
	candidates := map[string]matchingCandidate{}
	for _, id := range ids {
		d := l.classes[id]
		if !d.active || (d.role != "component" && d.role != "directive") {
			continue
		}
		c := matchingCandidate{d: d, unknown: keyUnknown(d, "selector")}
		text, _ := d.attrs["selector"].(string)
		c.absent = text == "" && !c.unknown
		if text != "" && !c.unknown {
			var ok bool
			c.selectors, ok = parseSelector(text)
			c.unmodeled = !ok
			if !ok {
				l.diagnostic(d, "selector", "angular_selector_unmodeled", "info", "selector is outside the modeled subset")
			}
		}
		candidates[id] = c
	}
	for _, id := range ids {
		d := l.classes[id]
		if !d.active || d.role != "component" {
			continue
		}
		t, _ := d.attrs["template"].(map[string]any)
		if t["status"] != "linked" && t["status"] != "parsed" {
			continue
		}
		file := d.file
		var f template.Facts
		if t["kind"] == "external" {
			file = t["path"].(string)
			f = l.templates[file]
		} else {
			for _, in := range d.inline {
				if in.Span == d.metadata["template"].Span && in.Facts != nil {
					f = *in.Facts
				}
			}
		}
		if !f.Valid() {
			continue
		}
		l.matchTemplate(d, file, f, b.host(d), candidates)
	}
}

func (l *linker) templateDiagnostic(d *declaration, file, code, severity, kind, name string) {
	l.diagnostics = append(l.diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Code: code, Severity: severity, Message: fmt.Sprintf("%s template %s %s", d.identity, kind, name)})
}

func (l *linker) matchTemplate(d *declaration, file string, f template.Facts, scope compilationScope, candidates map[string]matchingCandidate) {
	elements, structural, pipes := matchingCounts(false), matchingCounts(false), matchingCounts(true)
	recovery := recoveryFor(f, l.sources[file])
	if scope.status == "none" || scope.status == "unknown" || scope.status == "invalid" {
		l.templateDiagnostic(d, file, "angular_template_scope_unknown", "info", "scope", scope.reason)
	}
	if recovery.first != nil {
		l.templateDiagnostic(d, file, "angular_template_structure_unknown", "info", "structure", fmt.Sprintf("line %d: %s", recovery.first.StartLine, recovery.cause))
	}
	local := sortedScope(scope)
	matchedPairs := 0
	link := func(kind, target string, spans []contract.Span) {
		matchedPairs++
		for _, s := range spans {
			l.edges = append(l.edges, facts.Edge(kind, d.identity, target, file, s))
		}
	}
	classify := func(n matchingNode, usage bool, counts map[string]any, forcedInvalid bool) {
		if scope.status == "none" || scope.status == "invalid" || recovery.unknown(n.span) {
			if usage {
				addMatchingCount(counts, "unknown")
			}
			return
		}
		type match struct {
			candidate matchingCandidate
			spans     []contract.Span
		}
		matches := []match{}
		uncertain := n.uncertain || scope.status == "unknown" || nodeRecovery(f, n)
		components := 0
		for _, id := range local {
			c, ok := candidates[id]
			if !ok {
				continue
			}
			if c.unmodeled || c.unknown {
				uncertain = true
				continue
			}
			if c.absent {
				if c.d.role == "component" && n.name == "ng-component" {
					uncertain = true
				}
				continue
			}
			spans, yes, maybe := matchCandidate(n, c.selectors, f.ErrorSpans)
			uncertain = uncertain || maybe
			if yes {
				matches = append(matches, match{c, spans})
				if c.d.role == "component" {
					components++
				}
			}
		}
		invalid := forcedInvalid || components > 1
		if !forcedInvalid {
			for _, m := range matches {
				if components > 1 && m.candidate.d.role == "component" {
					continue
				}
				link("renders", m.candidate.d.identity, m.spans)
			}
		}
		status := "unmatched"
		switch {
		case invalid:
			status = "invalid"
		case len(matches) > 0:
			status = "linked"
		case uncertain:
			status = "unknown"
		case scope.status == "open":
			status = "external_scope"
		}
		kind := "element"
		if n.structural {
			kind = "structural"
		}
		if components > 1 {
			l.templateDiagnostic(d, file, "angular_template_multiple_components", "warning", kind, n.name)
		}
		if usage {
			addMatchingCount(counts, status)
			if status == "unmatched" {
				l.templateDiagnostic(d, file, "angular_template_unmatched", "info", kind, n.name)
			}
		}
	}
	for i, e := range f.Elements {
		usage := strings.Contains(e.Name, "-") && e.Name != "ng-container" && e.Name != "ng-template" && e.Name != "ng-content"
		if e.Dropped {
			if usage && e.Name == "link" {
				addMatchingCount(elements, "unknown")
			}
			continue
		}
		if e.Name == "ng-content" {
			continue
		}
		n := elementMatchingNode(i, f)
		classify(n, usage, elements, false)
		attrs := []template.Attribute{}
		for _, a := range f.Attributes {
			if a.Element == i && a.Kind == "structural" {
				attrs = append(attrs, a)
			}
		}
		if len(attrs) > 0 {
			a := attrs[0]
			sn := matchingNode{name: "ng-template", span: a.Span, nameSpan: a.Span, attrs: map[string]matchingAttribute{a.Name: {span: a.Span, valueSpan: a.Span}}, structural: true}
			if a.Value != "" {
				sn.secondary = a.Name
			}
			classify(sn, true, structural, len(attrs) > 1)
		}
	}
	for _, p := range f.Pipes {
		status := "unmatched"
		invalid := p.Track
		for _, a := range f.Attributes {
			if (a.Kind == "event" || a.Kind == "two_way") && a.ValueSpan != nil && containsSpan(*a.ValueSpan, p.Span) {
				invalid = true
			}
		}
		matches := []string{}
		unknownName := false
		for _, id := range local {
			t := l.classes[id]
			if t == nil || !t.active || t.role != "pipe" {
				continue
			}
			name, ok := t.attrs["name"].(string)
			if !ok || name == "" {
				unknownName = true
			}
			if name == p.Name {
				matches = append(matches, id)
			}
		}
		switch {
		case recovery.unknown(p.Span) || scope.status == "none" || scope.status == "invalid":
			status = "unknown"
		case invalid:
			status = "invalid"
		case p.Flagged || scope.status == "none" || scope.status == "unknown" || scope.status == "invalid":
			status = "unknown"
		case len(matches) == 1 && !unknownName && (scope.status == "complete" || scope.status == "open" && scope.pipeProvenAfterOpen(matches[0])):
			status = "linked"
			link("uses_pipe", matches[0], []contract.Span{p.Span})
		case len(matches) > 0 && (scope.status != "complete" || unknownName):
			status = "shadowable"
		case scope.status == "open":
			status = "external_scope"
		case len(matches) > 1 || unknownName:
			status = "unknown"
		}
		addMatchingCount(pipes, status)
		if status == "invalid" {
			l.templateDiagnostic(d, file, "angular_pipe_invalid_context", "warning", "pipe", p.Name)
		}
		if status == "unmatched" {
			l.templateDiagnostic(d, file, "angular_template_unmatched", "info", "pipe", p.Name)
		}
	}
	d.attrs["template_matching"] = map[string]any{"scope": scope.status, "matches": matchedPairs, "elements": elements, "structural": structural, "pipes": pipes, "recovery_spans": len(f.ErrorSpans)}
}

func elementMatchingNode(index int, f template.Facts) matchingNode {
	e := f.Elements[index]
	n := matchingNode{name: e.Name, span: e.Span, nameSpan: e.Span, attrs: map[string]matchingAttribute{}}
	n.nameSpan.StartColumn++
	n.nameSpan.EndLine, n.nameSpan.EndColumn = n.nameSpan.StartLine, n.nameSpan.StartColumn+len(e.Name)
	for _, c := range e.Name {
		if c == ':' || c >= 'A' && c <= 'Z' {
			n.uncertain = true
		}
	}
	put := func(name, value string, a template.Attribute, static bool) {
		if strings.ContainsAny(name, ".:") || strings.HasPrefix(name, "@") {
			n.uncertain = true
			return
		}
		uncertain := static && strings.ContainsFunc(value, func(r rune) bool { return r == '&' || r > unicode.MaxASCII })
		n.attrs[name] = matchingAttribute{value: strings.ToLower(value), span: a.NameSpan, valueSpan: a.Span, uncertainValue: uncertain}
	}
	for _, a := range f.Attributes {
		if a.Element != index {
			continue
		}
		if e.NonBindable {
			put(a.RawName, a.Value, a, true)
			continue
		}
		if a.Kind == "static" && a.RawName != "i18n" && !strings.HasPrefix(a.RawName, "i18n-") {
			put(a.RawName, a.Value, a, true)
		}
	}
	if e.NonBindable {
		return n
	}
	for _, a := range f.Attributes {
		if a.Element == index && (a.Kind == "property" || a.Kind == "two_way" || a.Kind == "interpolated") {
			put(a.Name, "", a, false)
		}
	}
	for _, a := range f.Attributes {
		if a.Element != index {
			continue
		}
		if a.Kind == "event" {
			put(a.Name, "", a, false)
		}
		if a.Kind == "two_way" {
			put(a.Name+"Change", "", a, false)
		}
	}
	return n
}

func matchCandidate(n matchingNode, selectors []selectorCompound, errors []contract.Span) ([]contract.Span, bool, bool) {
	uncertain := false
	// Namespaced/uppercase element names have no definite interpretation under N4.
	for _, c := range n.name {
		if c == ':' || c >= 'A' && c <= 'Z' {
			return nil, false, true
		}
	}
	spans := []contract.Span{}
	matched := false
	for _, s := range selectors {
		if s.element != "" && s.element != n.name {
			continue
		}
		yes, secondary := true, false
		parts := []contract.Span{}
		if s.element != "" {
			parts = append(parts, n.nameSpan)
		}
		for _, requirement := range s.attrs {
			a, ok := n.attrs[requirement.name]
			if !ok {
				if n.secondary != "" && strings.HasPrefix(requirement.name, n.secondary) && len(requirement.name) > len(n.secondary) && requirement.name[len(n.secondary)] >= 'A' && requirement.name[len(n.secondary)] <= 'Z' {
					secondary = true
					continue
				}
				yes = false
				break
			}
			if requirement.value != "" {
				if a.uncertainValue {
					uncertain = true
					yes = false
					break
				}
				if requirement.value != a.value {
					yes = false
					break
				}
				parts = append(parts, a.valueSpan)
			} else {
				parts = append(parts, a.span)
			}
		}
		if !yes {
			continue
		}
		if secondary {
			uncertain = true
			continue
		}
		if n.structural {
			parts = []contract.Span{n.span}
		}
		clean := true
		for _, part := range parts {
			for _, err := range errors {
				if intersectsSpan(part, err) {
					clean = false
				}
			}
		}
		if !clean {
			uncertain = true
			continue
		}
		matched = true
		spans = append(spans, parts...)
	}
	return spans, matched, uncertain
}

func nodeRecovery(f template.Facts, n matchingNode) bool {
	for _, err := range f.ErrorSpans {
		if !intersectsSpan(n.span, err) {
			continue
		}
		descendant := false
		if !n.structural {
			for _, e := range f.Elements {
				if e.Span != n.span && containsSpan(n.span, e.Span) && containsSpan(e.Span, err) && intersectsSpan(e.Span, err) {
					descendant = true
					break
				}
			}
		}
		if !descendant {
			return true
		}
	}
	return false
}

package clients

import (
	"strings"

	"github.com/graphvein/graphvein/internal/lang/facts"
)

// A spread must distinguish an absent own method from an unknown own method.
// Only absence preserves a method written before the spread.
type fetchMethodValue struct {
	present bool
	value   expr
}

func (r *resolver) applyWrapper(file string, call facts.ClientCall, w wrapper, identity string) request {
	args := append([]expr(nil), call.Expression.Args...)
	if w.request.library == "fetch" {
		for i := range args {
			if args[i].Kind == "undefined" {
				values := newEvaluation(r).expression(evaluationContext{file: file, scope: call.URLScope}, args[i])
				if len(values) != 1 || values[0].kind != "undefined" {
					args[i].Kind, args[i].Name = "identifier", "undefined"
				}
			}
		}
	}
	return w.apply(args, identity)
}

func (r *resolver) fetchMethod(file string, scope int, config expr) expr {
	if config.Kind == "" {
		return defaultMethod()
	}
	if config.Kind != "object" {
		// Keep the existing direct nonliteral-init boundary. Wrapper options
		// are substituted into object spreads before this point.
		return expr{Kind: "dynamic", Text: config.Text}
	}
	ev := newEvaluation(r)
	c := evaluationContext{file: file, scope: scope}
	method := ev.fetchObjectMethod(c, config)
	if !method.present {
		return defaultMethod()
	}
	var result string
	values := ev.expression(c, method.value)
	for i, v := range values {
		text := "GET"
		if v.kind != "undefined" {
			if !v.known() || v.kind == "object" {
				return expr{Kind: "dynamic", Text: method.value.Text}
			}
			text = strings.ToUpper(v.text)
		}
		if i > 0 && text != result {
			return expr{Kind: "dynamic", Text: method.value.Text}
		}
		result = text
	}
	if len(values) == 0 || ev.exhausted {
		return expr{Kind: "dynamic", Text: method.value.Text}
	}
	return expr{Kind: "literal", Name: result, Text: method.value.Text}
}

func (ev *evaluation) fetchObjectMethod(c evaluationContext, object expr) fetchMethodValue {
	var method fetchMethodValue
	entries := object.Keywords
	if len(entries) == 0 {
		// Also admit synthetic objects made from proven evaluator values.
		for _, key := range object.Keys {
			entries = append(entries, facts.RouteKeyword{Name: key, Value: object.Properties[key]})
		}
	}
	for _, entry := range entries {
		if !ev.step() {
			return fetchMethodValue{true, expr{Kind: "dynamic", Text: object.Text}}
		}
		if entry.Name != "" && entry.Value.Kind == "" {
			// Without spreads, the last own property is sufficient; do not
			// duplicate those values in the per-file syntax cache.
			entry.Value = object.Properties[entry.Name]
		}
		switch {
		case entry.Name == "method":
			method = fetchMethodValue{true, entry.Value}
		case entry.Name == "__proto__":
			method = fetchMethodValue{true, expr{Kind: "dynamic", Text: object.Text}}
		case entry.Name != "":
			// An ordinary non-method property does not affect the method.
		case entry.Value.Kind == "spread_element" && len(entry.Value.Parts) == 1:
			if next := ev.fetchSpreadMethod(c, entry.Value.Parts[0]); next.present {
				method = next
			}
		default:
			// Computed keys and accessors may contribute a method.
			method = fetchMethodValue{true, expr{Kind: "dynamic", Text: entry.Value.Text}}
		}
	}
	return method
}

func (ev *evaluation) fetchSpreadMethod(c evaluationContext, source expr) fetchMethodValue {
	if source.Kind == "object" {
		return ev.fetchObjectMethod(c, source)
	}
	values := ev.expression(c, source)
	var result fetchMethodValue
	for i, v := range values {
		next := fetchMethodValue{}
		switch v.kind {
		case "null", "undefined":
		case "object":
			if own, exists := v.properties["method"]; exists {
				next = fetchMethodValue{true, fetchValueExpr(own, source.Text)}
			}
		default:
			return fetchMethodValue{true, expr{Kind: "dynamic", Text: source.Text}}
		}
		if i > 0 && (next.present != result.present || next.value.Kind != result.value.Kind || next.value.Name != result.value.Name) {
			return fetchMethodValue{true, expr{Kind: "dynamic", Text: source.Text}}
		}
		result = next
	}
	if ev.exhausted || len(values) == 0 {
		return fetchMethodValue{true, expr{Kind: "dynamic", Text: source.Text}}
	}
	return result
}

func fetchValueExpr(v urlValue, text string) expr {
	if v.kind == "undefined" {
		// This value was already proven in its source scope; a caller's
		// binding named undefined must not capture it after freezing.
		return expr{Kind: "fetch_undefined", Text: text}
	}
	if v.kind == "null" {
		return expr{Kind: v.kind, Text: text}
	}
	if v.known() && v.kind != "object" {
		return expr{Kind: "literal", Name: v.text, Text: text}
	}
	if v.kind == "object" {
		e := expr{Kind: "object", Text: text, Keys: v.keys, Properties: map[string]expr{}}
		for _, key := range v.keys {
			e.Properties[key] = fetchValueExpr(v.properties[key], text)
		}
		return e
	}
	return expr{Kind: "dynamic", Text: text}
}

func (r *resolver) freezeFetchConfig(file string, scope int, e expr, params []facts.ClientParameter) expr {
	return r.freezeFetchConfigContext(evaluationContext{file: file, scope: scope}, e, params)
}

func (r *resolver) freezeFetchDefault(c evaluationContext, e expr) expr {
	switch e.Kind {
	case "", "object", "identifier", "undefined", "null", "literal":
		frozen := r.freezeFetchConfigContext(c, e, nil)
		if frozen.Kind == "null" {
			// Preserve proven default provenance through forwarded options;
			// a supplied non-object argument still has the unknown boundary.
			frozen.Kind = "fetch_null"
		}
		return frozen
	default:
		// A declared but unproven default is distinct from no default.
		return expr{Kind: "dynamic", Text: e.Text}
	}
}

func (r *resolver) freezeFetchConfigContext(c evaluationContext, e expr, params []facts.ClientParameter) expr {
	if e.Kind == "identifier" || e.Kind == "undefined" {
		for _, p := range params {
			if p.Name == e.Name {
				return e
			}
		}
		values := newEvaluation(r).expression(c, e)
		if len(values) == 1 {
			return fetchValueExpr(values[0], e.Text)
		}
		return expr{Kind: "dynamic", Text: e.Text}
	}
	// Clone before freezing: cached facts must survive replay unchanged.
	if e.Object != nil {
		object := r.freezeFetchConfigContext(c, *e.Object, params)
		e.Object = &object
	}
	e.Parts = append([]expr(nil), e.Parts...)
	for i := range e.Parts {
		e.Parts[i] = r.freezeFetchConfigContext(c, e.Parts[i], params)
	}
	e.Keywords = append([]facts.RouteKeyword(nil), e.Keywords...)
	for i := range e.Keywords {
		e.Keywords[i].Value = r.freezeFetchConfigContext(c, e.Keywords[i].Value, params)
	}
	if e.Properties != nil {
		properties := map[string]expr{}
		for key, value := range e.Properties {
			properties[key] = r.freezeFetchConfigContext(c, value, params)
		}
		e.Properties = properties
	}
	return e
}

func spreadReferences(e expr, name string) bool {
	if e.Kind == "spread_element" && len(e.Parts) == 1 {
		source := e.Parts[0]
		if source.Kind == "forwarded_options" && source.Object != nil {
			source = *source.Object
		}
		if source.Kind == "identifier" && source.Name == name {
			return true
		}
	}
	for _, entry := range e.Keywords {
		if spreadReferences(entry.Value, name) {
			return true
		}
	}
	return false
}

func (r *resolver) freezeEnvironmentBase(file string, scope int, e expr) expr {
	if r.baseShaped(file, scope, e) {
		return expr{Kind: "environment_base", Text: e.Text}
	}
	e.Parts = append([]expr(nil), e.Parts...)
	for i := range e.Parts {
		e.Parts[i] = r.freezeEnvironmentBase(file, scope, e.Parts[i])
	}
	return e
}

package clients

import (
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

func generatedURL(text string) expr {
	e := expr{Kind: "template_string"}
	for {
		start := strings.Index(text, "${")
		if start < 0 {
			e.Parts = append(e.Parts, expr{Kind: "literal", Name: text})
			break
		}
		e.Parts = append(e.Parts, expr{Kind: "literal", Name: text[:start]})
		end := strings.IndexByte(text[start:], '}')
		if end < 0 {
			return expr{Kind: "unknown"}
		}
		// This is a parameter of the predicted generated function, never a
		// binding in the config or mutator's lexical scope.
		e.Parts = append(e.Parts, expr{Kind: "orval_parameter", Name: text[start+2 : start+end]})
		text = text[start+end+1:]
	}
	return e
}

func forwardsOptions(e expr, name string) bool {
	if e.Kind == "identifier" && e.Name == name {
		return true
	}
	if e.Kind != "object" || len(e.Keywords) == 0 {
		return false
	}
	spreads := 0
	for i, entry := range e.Keywords {
		if entry.Name == "method" {
			return false
		}
		if entry.Name != "" {
			continue
		}
		v := entry.Value
		if v.Kind != "spread_element" || i != 0 || len(v.Parts) != 1 || v.Parts[0].Kind != "identifier" || v.Parts[0].Name != name {
			return false
		}
		spreads++
	}
	return spreads == 1
}

func (r *resolver) orvalTransport(p orvalProject, op orvalOperation) (map[string]any, *evaluation, *contract.Evidence, string) {
	q := request{library: "fetch", url: generatedURL(op.url), method: expr{Kind: "literal", Name: op.method}}
	file, scope, urlScope := p.config, 0, 0
	var env *environment
	var evidence *contract.Evidence
	if p.mutator != "" {
		_, mf, name, err := contract.ParseIdentity(p.mutator)
		if err != nil {
			return nil, nil, nil, "invalid mutator identity"
		}
		b, _, ok, _ := newEvaluation(r).binding(evaluationContext{file: mf}, name)
		proof := b.Value
		if !ok || proof.Kind != "orval_transport" || proof.Invalid || proof.Span == nil {
			reason := proof.Text
			if reason == "" {
				reason = "no bounded top-level fetch prefix"
			}
			return nil, nil, nil, reason
		}
		if len(proof.Args) < 2 || !forwardsOptions(proof.Args[1], proof.Operator) {
			return nil, nil, nil, "fetch init does not preserve the generated method"
		}
		var send facts.ClientCall
		found := false
		for _, c := range r.files[mf].Routes.Clients {
			if c.Span == *proof.Span {
				send = c
				found = true
				break
			}
		}
		if !found {
			return nil, nil, nil, "fetch source site is unavailable"
		}
		recognized, ok := r.recognize(mf, send)
		if !ok || recognized.library != "fetch" {
			return nil, nil, nil, "transport is not an unshadowed global fetch"
		}
		q.url = proof.Args[0]
		file = mf
		scope = send.Scope
		urlScope = send.URLScope
		env = &environment{values: map[string]localValue{proof.Name: {values: newEvaluation(r).expression(evaluationContext{file: p.config}, generatedURL(op.url))}}}
		evidence = &contract.Evidence{File: mf, Span: *proof.Span}
	}
	attrs, ev := r.pathAttributesEnv(file, scope, urlScope, q, op.method, true, nil, env)
	paths := attrs["paths"].([]map[string]any)
	if len(paths) != 1 || paths[0]["template"] == nil {
		return nil, nil, nil, "URL evaluation is dynamic"
	}
	return attrs, ev, evidence, ""
}

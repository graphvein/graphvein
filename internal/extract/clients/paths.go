package clients

import (
	"fmt"
	"slices"
	"sort"

	"github.com/graphvein/graphvein/internal/contract"
)

func valueExpr(v urlValue, original expr) expr {
	if v.kind == "string" {
		if allLiteral(v.pieces) {
			return expr{Kind: "literal", Name: v.text, Text: original.Text}
		}
		e := expr{Kind: "template_string", Text: original.Text}
		for _, p := range v.pieces {
			if p.literal {
				if p.text == "" {
					continue
				}
				e.Parts = append(e.Parts, expr{Kind: "literal", Name: p.text})
			} else {
				e.Parts = append(e.Parts, p.expression)
			}
		}
		return e
	}
	if v.kind == "dynamic" && len(v.pieces) == 1 {
		return v.pieces[0].expression
	}
	// fetch/axios do not receive a URL string for unsupported bare value kinds.
	return expr{Kind: "unknown", Text: original.Text}
}

func (r *resolver) pathAttributes(file string, scope, urlScope int, q request, method string, static bool, bases map[string]string) (map[string]any, *evaluation) {
	return r.pathAttributesEnv(file, scope, urlScope, q, method, static, bases, nil)
}

func (r *resolver) pathAttributesEnv(file string, scope, urlScope int, q request, method string, static bool, bases map[string]string, env *environment) (map[string]any, *evaluation) {
	ev := newEvaluation(r)
	values := ev.expression(evaluationContext{file: file, scope: urlScope, env: env}, q.url)
	resolution := "literal"
	if ev.constant {
		resolution = "constant"
	}
	if ev.evaluated {
		resolution = "evaluated"
	}
	if ev.exhausted {
		values = valueSet{unknownValue(q.url)}
	}
	var paths []map[string]any
	// expressions holds each entry's base expression while values merge
	// into it; mixed marks an entry whose values disagree on it.
	var expressions []any
	var mixed []bool
	attrs := map[string]any{"library": q.library, "method": nil, "base": "unresolved", "status": "dynamic"}
	if static {
		attrs["method"] = method
	} else {
		attrs["method_expression"] = compact(q.method.Text)
	}
	for _, v := range values {
		if q.swr && v.kind == "null" {
			// SWR does not fetch for a null key (row NX-L2).
			continue
		}
		request := q
		request.url = valueExpr(v, q.url)
		attributeScope := scope
		if q.library == "hey-api" {
			// hey-api baseURL uses the bounded evaluator's lexical scopes.
			attributeScope = urlScope
		}
		a := r.attributes(file, attributeScope, request, method, static, bases)
		entry := map[string]any{"template": a["path_template"], "origin": a["origin"], "conditional": v.conditional, "resolution": resolution, "status": "dynamic", "match": nil, "candidates": []string{}, "candidate_count": 0}
		if v.originResolution == "unresolved" {
			entry["origin"] = nil
			entry["origin_resolution"] = "unresolved"
			a["base"] = "none"
			if v.originBase != "" {
				// The base supplies the origin only: the path is the
				// object's own (docs/extract/clients.md "Build-time
				// constant bases").
				a["base"] = "unresolved"
				a["base_expression"] = v.originBase
			}
		}
		if a["trailing_slash"] == true {
			entry["trailing_slash"] = true
		}
		key := fmt.Sprint(entry["template"], entry["trailing_slash"])
		if base, ok := ev.bases[key]; !ok || a["base"] == "unresolved" || base == "none" {
			ev.bases[key] = a["base"].(string)
		}
		duplicate := false
		for i, p := range paths {
			if p["template"] == entry["template"] && p["trailing_slash"] == entry["trailing_slash"] {
				p["conditional"] = p["conditional"] == true || entry["conditional"] == true
				if entry["origin"] == nil {
					p["origin"] = nil
				}
				if expressions[i] != a["base_expression"] {
					mixed[i] = true
				}
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		paths = append(paths, entry)
		expressions = append(expressions, a["base_expression"])
		mixed = append(mixed, false)
		attrs["base"] = a["base"]
		if a["base_expression"] != nil {
			attrs["base_expression"] = a["base_expression"]
		}
	}
	alternativeBases(paths, expressions, mixed, ev)
	if len(paths) > 4 || len(paths) == 0 {
		conditional := false
		for _, p := range paths {
			conditional = conditional || p["conditional"] == true
		}
		paths = []map[string]any{{"template": nil, "origin": nil, "conditional": conditional, "resolution": resolution, "status": "dynamic", "match": nil, "candidates": []string{}, "candidate_count": 0}}
	}
	sort.Slice(paths, func(i, j int) bool {
		a, aok := paths[i]["template"].(string)
		b, bok := paths[j]["template"].(string)
		if aok != bok {
			return aok
		}
		if a != b {
			return a < b
		}
		return paths[i]["trailing_slash"] != true && paths[j]["trailing_slash"] == true
	})
	allNull := true
	for _, p := range paths {
		allNull = allNull && p["template"] == nil
	}
	if allNull {
		attrs["base"] = "unresolved"
		delete(attrs, "base_expression")
	}
	attrs["paths"] = paths
	helpers := ev.helperIDs()
	if len(helpers) > 0 {
		if len(helpers) > 10 {
			helpers = helpers[:10]
		}
		attrs["helpers"] = helpers
	}
	return attrs, ev
}

// alternativeBases stores each alternative's base on calls whose call-level
// base does not decide it: more than one alternative, or a conditional one.
// base is the kind the matcher uses for the alternative (linkPaths);
// base_expression is its unresolved base's expression when every value
// merged into the alternative has the same one, and is omitted otherwise.
// The project link pass (ADR 0075) matches each alternative from these.
func alternativeBases(paths []map[string]any, expressions []any, mixed []bool, ev *evaluation) {
	several := len(paths) > 1
	for _, p := range paths {
		several = several || p["conditional"] == true
	}
	if !several {
		return
	}
	for i, p := range paths {
		if p["template"] == nil {
			continue
		}
		base := ev.bases[fmt.Sprint(p["template"], p["trailing_slash"])]
		p["base"] = base
		if base == "unresolved" && !mixed[i] && expressions[i] != nil {
			p["base_expression"] = expressions[i]
		}
	}
}

func (m matcher) linkPaths(identity, file string, span contract.Span, attrs map[string]any, ev *evaluation) []contract.Edge {
	paths := attrs["paths"].([]map[string]any)
	byTarget := map[string]contract.Edge{}
	for _, p := range paths {
		p["scope"] = attrs["scope"]
		p["method"] = attrs["method"]
		stored, hasBase := p["base"]
		p["base"] = ev.bases[fmt.Sprint(p["template"], p["trailing_slash"])]
		p["path_template"] = p["template"]
		links := m.link(identity, file, span, p)
		for _, edge := range links {
			if old, exists := byTarget[edge.Target]; !exists || edge.Confidence > old.Confidence {
				edge.Evidence = append(edge.Evidence, ev.evidenceList()...)
				byTarget[edge.Target] = edge
			}
			p["candidates"] = []string{edge.Target}
		}
		delete(p, "method")
		delete(p, "base")
		if hasBase {
			p["base"] = stored
		}
		delete(p, "path_template")
		delete(p, "scope")
		delete(p, unknownPrefixTier)
	}
	// The assumptions of the linked alternatives, once each in path order:
	// Next.js ones (NX-V1, NX-V3) and the matched route path entry's own
	// (GR-V5, RO-V1, RO-V2) first, then the unknown-prefix
	// tier's (rows UP-19, UP-V1, UP-V2, with the entry's path assumptions
	// after them).
	for _, tier := range []bool{false, true} {
		for _, p := range paths {
			if p["assumes"] == nil || p["status"] != "linked" || (p["route_prefix"] == "unresolved") != tier {
				continue
			}
			list, _ := attrs["assumes"].([]string)
			for _, id := range p["assumes"].([]string) {
				if !slices.Contains(list, id) {
					list = append(list, id)
				}
			}
			attrs["assumes"] = list
		}
	}
	// The origin assumptions of the linked alternatives (row LF-1's
	// shown assumption), once each, in path order.
	var assumed []string
	for _, p := range paths {
		if text, ok := p["origin_assumption"].(string); ok && p["status"] == "linked" && !slices.Contains(assumed, text) {
			assumed = append(assumed, text)
		}
	}
	if len(assumed) > 0 {
		attrs["origin_assumptions"] = assumed
	}
	attrs["status"] = "linked"
	for _, status := range []string{"dynamic", "unmatched", "test_only", "ambiguous"} {
		found := false
		for _, p := range paths {
			found = found || p["status"] == status
		}
		if found {
			attrs["status"] = status
			break
		}
	}
	ids := make([]string, 0, len(byTarget))
	for id := range byTarget {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []contract.Edge
	for _, id := range ids {
		out = append(out, byTarget[id])
	}
	return out
}

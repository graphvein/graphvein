package clients

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

type urlValue struct {
	kind, text                 string
	pieces                     []piece
	keys                       []string
	properties                 map[string]urlValue
	literalObject, conditional bool
	source                     *contract.Evidence
	originResolution           string
	// originBase is the base expression of an unresolved URL-object origin
	// (urlObjectBase), empty when no single base-shaped expression
	// supplies it.
	originBase string
}
type valueSet []urlValue
type localValue struct {
	values  valueSet
	mutable bool
}
type environment struct {
	parent *environment
	values map[string]localValue
}

func (e *environment) get(name string) (localValue, *environment, bool) {
	for at := e; at != nil; at = at.parent {
		if v, ok := at.values[name]; ok {
			return v, at, true
		}
	}
	return localValue{}, nil, false
}

type evaluation struct {
	r                              *resolver
	steps, depth                   int
	urlAliases                     int
	evaluated, constant, exhausted bool
	active                         map[string]bool
	helpers                        map[string]bool
	evidence                       map[string]contract.Evidence
	bases                          map[string]string
}
type evaluationContext struct {
	file  string
	scope int
	env   *environment
	table bool
}

func newEvaluation(r *resolver) *evaluation {
	return &evaluation{r: r, active: map[string]bool{}, helpers: map[string]bool{}, evidence: map[string]contract.Evidence{}, bases: map[string]string{}}
}
func (ev *evaluation) step() bool {
	ev.steps++
	if ev.steps > 2000 {
		ev.exhausted = true
		return false
	}
	return true
}
func unknownValue(e expr) urlValue {
	return urlValue{kind: "dynamic", pieces: []piece{{expression: e}}}
}
func stringValue(s string) urlValue {
	return urlValue{kind: "string", text: s, pieces: []piece{{literal: true, text: s}}}
}
func (v urlValue) known() bool {
	return v.kind != "dynamic" && v.kind != "unknown" && (v.kind != "string" || allLiteral(v.pieces))
}
func allLiteral(ps []piece) bool {
	for _, p := range ps {
		if !p.literal {
			return false
		}
	}
	return true
}
func (v urlValue) truth() bool {
	switch v.kind {
	case "undefined", "null":
		return false
	case "boolean":
		return v.text == "true"
	case "integer":
		return v.text != "0"
	case "string":
		return v.text != ""
	case "object":
		return true
	}
	return false
}
func (v urlValue) stringify() []piece {
	switch v.kind {
	case "string", "dynamic":
		return v.pieces
	case "object":
		// Own conversion hooks can throw or change ToPrimitive. Do not guess.
		if _, ok := v.properties["toString"]; ok {
			return []piece{{expression: expr{Kind: "unknown"}}}
		}
		if _, ok := v.properties["valueOf"]; ok {
			return []piece{{expression: expr{Kind: "unknown"}}}
		}
		return []piece{{literal: true, text: "[object Object]"}}
	case "undefined", "null", "boolean", "integer":
		return []piece{{literal: true, text: v.text}}
	}
	return []piece{{expression: expr{Kind: "unknown"}}}
}
func concatenate(a, b urlValue) urlValue {
	ps := append(append([]piece(nil), a.stringify()...), b.stringify()...)
	v := urlValue{kind: "string", pieces: ps, conditional: a.conditional || b.conditional}
	if allLiteral(ps) {
		for _, p := range ps {
			v.text += p.text
		}
	}
	return v
}
func (ev *evaluation) addEvidence(file string, s *contract.Span) {
	if s == nil {
		return
	}
	v := contract.Evidence{File: file, Span: *s}
	ev.evidence[siteKey(file, *s)] = v
}
func (ev *evaluation) binding(c evaluationContext, name string) (facts.RouteBinding, int, bool, bool) {
	d := ev.r.files[c.file].Routes
	if d == nil {
		return facts.RouteBinding{}, 0, false, false
	}
	nested := false
	for at := c.scope; at >= 0 && at < len(d.URLScopes); at = d.URLScopes[at].Parent {
		if b, ok := d.URLScopes[at].Bindings[name]; ok {
			return b, at, true, nested
		}
		nested = nested || d.URLScopes[at].Function
	}
	return facts.RouteBinding{}, 0, false, false
}
func (ev *evaluation) imported(b typescript.ExportBinding) (evaluationContext, facts.RouteBinding, bool) {
	if b.External || b.Ambiguous || b.Namespace || b.File == "" {
		return evaluationContext{}, facts.RouteBinding{}, false
	}
	c := evaluationContext{file: b.File}
	v, _, ok, _ := ev.binding(c, b.Name)
	return c, v, ok
}
func (ev *evaluation) expression(c evaluationContext, e expr) valueSet {
	if !ev.step() {
		return valueSet{unknownValue(e)}
	}
	dynamic := func() valueSet { return valueSet{unknownValue(e)} }
	switch e.Kind {
	case "fetch_null":
		return valueSet{{kind: "null", text: "null"}}
	case "fetch_undefined":
		return valueSet{{kind: "undefined", text: "undefined"}}
	case "fetch_options_method":
		if e.Object == nil || e.Object.Kind != "object" {
			return dynamic()
		}
		method := ev.fetchObjectMethod(c, *e.Object)
		if !method.present {
			return valueSet{{kind: "undefined", text: "undefined"}}
		}
		return ev.expression(c, method.value)
	case "url_object":
		return ev.urlObject(c, e)
	case "":
		return valueSet{{kind: "undefined", text: "undefined"}}
	case "literal":
		return valueSet{stringValue(e.Name)}
	case "true", "false":
		ev.evaluated = true
		return valueSet{{kind: "boolean", text: e.Kind}}
	case "null":
		ev.evaluated = true
		return valueSet{{kind: "null", text: "null"}}
	case "undefined":
		e.Kind = "identifier"
		e.Name = "undefined"
		return ev.expression(c, e)
	case "number":
		ev.evaluated = true
		s := strings.ReplaceAll(e.Name, "_", "")
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			if i, err2 := strconv.ParseInt(s, 0, 64); err2 == nil {
				v = float64(i)
				err = nil
			}
		}
		if err != nil || math.IsInf(v, 0) || math.IsNaN(v) || math.Trunc(v) != v {
			return dynamic()
		}
		return valueSet{{kind: "integer", text: numberString(v)}}
	case "identifier":
		if v, _, ok := c.env.get(e.Name); ok {
			return v.values
		}
		b, at, ok, nested := ev.binding(c, e.Name)
		if !ok {
			if e.Name == "undefined" {
				ev.evaluated = true
				return valueSet{{kind: "undefined", text: "undefined"}}
			}
			return dynamic()
		}
		if b.Module != "" {
			target := ev.r.modules.ImportBinding(c.file, b.Module, b.Export, "")
			var valid bool
			c, b, valid = ev.imported(target)
			if !valid {
				return dynamic()
			}
			at = 0
		}
		v := b.Evaluation
		if v == nil || !v.Constant || at != 0 && !v.Single || v.Mutated || v.Helper != nil || (at != 0 && v.Offset >= e.Offset && !nested) {
			return dynamic()
		}
		key := fmt.Sprintf("binding:%s:%d:%s", c.file, at, e.Name)
		if ev.active[key] {
			return dynamic()
		}
		ev.active[key] = true
		defer delete(ev.active, key)
		if at != 0 || v.Value.Kind != "literal" && !envBase(v.Value) {
			ev.evaluated = true
		} else {
			ev.constant = true
		}
		if at == 0 && envBase(v.Value) {
			// Keep the spelling at the request site instead of the compacted
			// initializer, whose source text is intentionally absent in caches.
			return valueSet{unknownValue(e)}
		}
		c.scope = at
		aliasCost := 1
		if at == 0 {
			aliasCost = 2
		}
		ev.urlAliases += aliasCost
		defer func() { ev.urlAliases -= aliasCost }()
		if at == 0 {
			c.env = nil
			c.table = v.Value.Kind == "object"
		}
		return ev.expression(c, v.Value)
	case "unary_expression":
		ev.evaluated = true
		if len(e.Parts) != 1 || (e.Operator != "-" && e.Operator != "+") {
			return dynamic()
		}
		values := ev.expression(c, e.Parts[0])
		for i := range values {
			if values[i].kind != "integer" {
				return dynamic()
			}
			n, err := strconv.ParseFloat(values[i].text, 64)
			if err != nil {
				return dynamic()
			}
			if e.Operator == "-" {
				n = -n
			}
			values[i].text = numberString(n)
		}
		return values
	case "template_string":
		if e.Invalid {
			return dynamic()
		}
		out := valueSet{stringValue("")}
		for _, p := range e.Parts {
			out = ev.combine(out, ev.expression(c, p), concatenate)
		}
		return out
	case "binary_expression":
		if len(e.Parts) != 2 {
			return dynamic()
		}
		left := ev.expression(c, e.Parts[0])
		var out valueSet
		for _, a := range left {
			switch e.Operator {
			case "+":
				for _, b := range ev.expression(c, e.Parts[1]) {
					if !ev.step() {
						return dynamic()
					}
					if a.kind == "string" || b.kind == "string" {
						if a.kind != "string" || b.kind != "string" {
							ev.evaluated = true
						}
						out = append(out, concatenate(a, b))
					} else {
						out = append(out, unknownValue(e))
					}
				}
			case "||", "??":
				if !envBase(e) {
					ev.evaluated = true
				}
				if !a.known() {
					out = append(out, unknownValue(e))
					continue
				}
				fall := e.Operator == "||" && !a.truth() || e.Operator == "??" && (a.kind == "null" || a.kind == "undefined")
				if fall {
					out = append(out, ev.expression(c, e.Parts[1])...)
				} else {
					out = append(out, a)
				}
			default:
				return dynamic()
			}
		}
		return out
	case "ternary_expression":
		ev.evaluated = true
		if len(e.Parts) != 3 {
			return dynamic()
		}
		var out valueSet
		for _, cond := range ev.expression(c, e.Parts[0]) {
			if cond.known() {
				at := 2
				if cond.truth() {
					at = 1
				}
				values := ev.expression(c, e.Parts[at])
				for i := range values {
					values[i].conditional = values[i].conditional || cond.conditional
				}
				out = append(out, values...)
			} else {
				for _, branch := range e.Parts[1:] {
					values := ev.expression(c, branch)
					for i := range values {
						values[i].conditional = true
					}
					out = append(out, values...)
				}
			}
		}
		return out
	case "object":
		ev.evaluated = true
		if e.Invalid {
			return dynamic()
		}
		out := valueSet{{kind: "object", properties: map[string]urlValue{}, keys: append([]string(nil), e.Keys...), literalObject: true}}
		for _, key := range e.Keys {
			p := e.Properties[key]
			values := ev.expression(c, p)
			var next valueSet
			for _, o := range out {
				for _, v := range values {
					if !ev.step() {
						return dynamic()
					}
					props := map[string]urlValue{}
					for k, val := range o.properties {
						props[k] = val
					}
					if c.table && p.Span != nil {
						v.source = &contract.Evidence{File: c.file, Span: *p.Span}
					}
					props[key] = v
					o2 := o
					o2.properties = props
					o2.conditional = o.conditional || v.conditional
					next = append(next, o2)
				}
			}
			out = next
		}
		return out
	case "member_expression", "subscript_expression":
		if envMember(e) {
			return dynamic()
		}
		if e.Object == nil {
			return dynamic()
		}
		if e.Kind == "member_expression" && e.Object.Kind == "identifier" {
			b, _, ok, _ := ev.binding(c, e.Object.Name)
			if ok && b.Module != "" {
				target := ev.r.modules.ImportBinding(c.file, b.Module, b.Export, e.Name)
				if at, b, valid := ev.imported(target); valid {
					v := b.Evaluation
					if v != nil && v.Constant && !v.Mutated {
						ev.evaluated = true
						return ev.expression(at, v.Value)
					}
				}
			}
		}
		objects := ev.expression(c, *e.Object)
		keys := valueSet{stringValue(e.Name)}
		if e.Kind == "subscript_expression" {
			if len(e.Parts) != 1 {
				return dynamic()
			}
			keys = ev.expression(c, e.Parts[0])
		}
		var out valueSet
		for _, o := range objects {
			for _, k := range keys {
				if o.kind == "object" && k.known() && (k.kind == "string" || k.kind == "integer") {
					if v, ok := o.properties[k.text]; ok {
						ev.evaluated = true
						if v.source != nil {
							ev.addEvidence(v.source.File, &v.source.Span)
						}
						v.conditional = v.conditional || o.conditional || k.conditional
						out = append(out, v)
						continue
					}
				}
				out = append(out, unknownValue(e))
			}
		}
		return out
	case "call_expression":
		return ev.helper(c, e)
	}
	return dynamic()
}
func numberString(v float64) string {
	if v == 0 {
		return "0"
	}
	if math.Abs(v) < 1e21 {
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	s := strconv.FormatFloat(v, 'e', -1, 64)
	parts := strings.Split(s, "e")
	exponent, _ := strconv.Atoi(parts[1])
	return parts[0] + "e" + fmt.Sprintf("%+d", exponent)
}
func (ev *evaluation) combine(a, b valueSet, op func(urlValue, urlValue) urlValue) valueSet {
	var out valueSet
	for _, x := range a {
		for _, y := range b {
			if !ev.step() {
				return valueSet{{kind: "unknown"}}
			}
			out = append(out, op(x, y))
		}
	}
	return out
}

func (ev *evaluation) helper(c evaluationContext, e expr) valueSet {
	caller := c
	dynamic := func() valueSet { return valueSet{unknownValue(e)} }
	if e.Object == nil || ev.depth >= 3 {
		return dynamic()
	}
	callee := *e.Object
	var b facts.RouteBinding
	ok := false
	// moduleLevel: the helper is bound at its module's top level (row
	// UP-39 reads only such helpers). autoImported: the callee is an
	// unbound name resolved through an auto-import declaration (row UP-41).
	moduleLevel, autoImported := true, false
	if callee.Kind == "identifier" {
		var at int
		var nested bool
		b, at, ok, nested = ev.binding(c, callee.Name)
		if ok && at != 0 && b.Evaluation != nil && b.Evaluation.Helper != nil && b.Evaluation.Helper.BoundConst && (!b.Evaluation.Single || b.Evaluation.Offset >= callee.Offset && !nested) {
			return dynamic()
		}
		moduleLevel = at == 0
		if ok && b.Module != "" {
			c, b, ok = ev.imported(ev.r.modules.ImportBinding(c.file, b.Module, b.Export, ""))
		}
		if !ok {
			// An unbound name a generated auto-import declaration binds
			// (row UP-41): read only through row UP-39 below.
			if declaration, module, found := ev.r.autoImport(c.file, callee.Name); found {
				c, b, ok = ev.imported(ev.r.modules.ImportBinding(declaration, module, callee.Name, ""))
				autoImported, moduleLevel = ok, true
			}
		}
	}
	if callee.Kind == "member_expression" && callee.Object != nil && callee.Object.Kind == "identifier" {
		binding, _, found, _ := ev.binding(c, callee.Object.Name)
		if found && binding.Module != "" {
			c, b, ok = ev.imported(ev.r.modules.ImportBinding(c.file, binding.Module, binding.Export, callee.Name))
		}
	}
	if !ok || b.Evaluation == nil || b.Evaluation.Mutated || b.Evaluation.Helper == nil || !b.Evaluation.Helper.Eligible {
		return dynamic()
	}
	h := b.Evaluation.Helper
	if ev.active[h.Identity] || len(ev.r.files[c.file].Repeated[h.Identity]) > 1 {
		return dynamic()
	}
	if autoImported {
		upCallTag("UP-41")
	}
	if values, read := ev.runtimeBase(caller, c, e, h, moduleLevel); read {
		return values
	}
	if autoImported {
		// The auto-import binding serves row UP-39 only: any other
		// reading of the helper stays as before (the name is unbound).
		return dynamic()
	}
	ev.evaluated = true
	// Evaluate arguments in the caller, before entering the helper's lexical scope.
	return ev.invoke(caller, c, e, h)
}

func (ev *evaluation) invoke(caller, target evaluationContext, e expr, h *facts.URLHelper) valueSet {
	args := make([]valueSet, len(e.Args))
	for i, a := range e.Args {
		if a.Kind == "spread_element" {
			return valueSet{unknownValue(e)}
		}
		args[i] = ev.expression(caller, a)
		if a.Kind != "object" {
			for j := range args[i] {
				if args[i][j].kind == "object" {
					args[i][j].literalObject = false
				}
			}
		}
	}
	contexts := []evaluationContext{{file: target.file, scope: h.Scope, env: &environment{values: map[string]localValue{}}}}
	for i, p := range h.Parameters {
		if p.Name == "" {
			return valueSet{unknownValue(e)}
		}
		values := valueSet{{kind: "undefined", text: "undefined"}}
		if i < len(args) {
			values = args[i]
		}
		var next []evaluationContext
		for _, c := range contexts {
			for _, v := range values {
				defaults := valueSet{v}
				if v.kind == "undefined" && p.Default.Kind != "" {
					defaults = ev.expression(c, p.Default)
				}
				for _, value := range defaults {
					if !ev.step() {
						return valueSet{unknownValue(e)}
					}
					env := &environment{values: map[string]localValue{}}
					for k, v := range c.env.values {
						env.values[k] = v
					}
					env.values[p.Name] = localValue{values: valueSet{value}}
					next = append(next, evaluationContext{file: c.file, scope: c.scope, env: env})
				}
			}
		}
		contexts = next
	}
	var out valueSet
	ev.active[h.Identity] = true
	ev.depth++
	defer func() { ev.depth--; delete(ev.active, h.Identity) }()
	for _, c := range contexts {
		key := fmt.Sprintf("%d:%s", ev.depth, memoKey(h.Identity, c.env))
		if v, ok := ev.r.memo[key]; ok {
			ev.helpers[h.Identity] = true
			for _, s := range v.evidence {
				ev.evidence[siteKey(s.File, s.Span)] = s
			}
			for _, id := range v.helpers {
				ev.helpers[id] = true
			}
			out = append(out, v.values...)
			continue
		}
		parentHelpers, parentEvidence := ev.helpers, ev.evidence
		ev.helpers = map[string]bool{}
		ev.evidence = map[string]contract.Evidence{}
		values, returned, valid := ev.statements(c, h.Body)
		if valid && returned && !ev.exhausted {
			ev.helpers[h.Identity] = true
		}
		usedHelpers, usedEvidence := ev.helperIDs(), ev.evidenceList()
		for id := range ev.helpers {
			parentHelpers[id] = true
		}
		for key, evidence := range ev.evidence {
			parentEvidence[key] = evidence
		}
		ev.helpers = parentHelpers
		ev.evidence = parentEvidence
		if !valid || !returned || ev.exhausted {
			return valueSet{unknownValue(e)}
		}
		out = append(out, values...)
		// Memoization belongs to this link pass. Include nesting depth in the key
		// so a shallow successful result cannot bypass the three-level bound.
		if ev.r.memo != nil {
			ev.r.memo[key] = memoValue{values: values, helpers: usedHelpers, evidence: usedEvidence}
		}
	}
	return out
}

type memoValue struct {
	values   valueSet
	helpers  []string
	evidence []contract.Evidence
}

func memoKey(id string, env *environment) string {
	keys := make([]string, 0, len(env.values))
	for k := range env.values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var s strings.Builder
	s.WriteString(id)
	for _, k := range keys {
		s.WriteString("|" + k)
		for _, v := range env.values[k].values {
			s.WriteString(valueKey(v))
		}
	}
	return s.String()
}
func valueKey(v urlValue) string {
	props := map[string]string{}
	for k, p := range v.properties {
		props[k] = valueKey(p)
	}
	data, _ := json.Marshal(struct {
		Kind, Text           string
		Pieces, Keys         []string
		Object               map[string]string
		Literal, Conditional bool
	}{v.kind, v.text, pieceKeys(v.pieces), v.keys, props, v.literalObject, v.conditional})
	return string(data)
}
func pieceKeys(ps []piece) []string {
	var out []string
	for _, p := range ps {
		out = append(out, fmt.Sprintf("%t:%s:%s:%s", p.literal, p.text, p.expression.Kind, p.expression.Name))
	}
	return out
}
func (ev *evaluation) helperIDs() []string {
	out := make([]string, 0, len(ev.helpers))
	for id := range ev.helpers {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
func (ev *evaluation) evidenceList() []contract.Evidence {
	out := make([]contract.Evidence, 0, len(ev.evidence))
	for _, v := range ev.evidence {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return siteKey(out[i].File, out[i].Span) < siteKey(out[j].File, out[j].Span) })
	return out
}

func ownKeys(v urlValue) []string {
	keys := append([]string(nil), v.keys...)
	index := func(s string) (uint64, bool) {
		i, err := strconv.ParseUint(s, 10, 32)
		return i, err == nil && i < 4294967295 && strconv.FormatUint(i, 10) == s
	}
	sort.SliceStable(keys, func(i, j int) bool {
		a, ai := index(keys[i])
		b, bi := index(keys[j])
		if ai && bi {
			return a < b
		}
		return ai && !bi
	})
	return keys
}
func (ev *evaluation) statements(c evaluationContext, body []facts.URLStatement) (valueSet, bool, bool) {
	for _, s := range body {
		if !ev.step() {
			return nil, false, false
		}
		switch s.Kind {
		case "declaration":
			if _, exists := c.env.values[s.Name]; exists {
				return nil, false, false
			}
			c.env.values[s.Name] = localValue{values: ev.expression(c, s.Expression), mutable: !s.Constant}
		case "assignment":
			v, env, ok := c.env.get(s.Name)
			if !ok || !v.mutable {
				return nil, false, false
			}
			values := ev.expression(c, s.Expression)
			if s.Operator == "+=" {
				values = ev.combine(v.values, values, func(a, b urlValue) urlValue {
					if a.kind != "string" && b.kind != "string" {
						return unknownValue(s.Expression)
					}
					return concatenate(a, b)
				})
			}
			env.values[s.Name] = localValue{values: values, mutable: true}
		case "return_statement":
			values := ev.expression(c, s.Expression)
			ev.addEvidence(c.file, &s.Span)
			return values, true, true
		case "if_statement":
			conditions := ev.expression(c, s.Expression)
			if len(conditions) != 1 || !conditions[0].known() {
				return nil, false, false
			}
			branch := s.Else
			if conditions[0].truth() {
				branch = s.Body
			}
			child := c
			child.env = &environment{parent: c.env, values: map[string]localValue{}}
			v, returned, ok := ev.statements(child, branch)
			if !ok || returned {
				return v, returned, ok
			}
		case "for_in_statement":
			objectExpr := s.Expression
			valuesMode := false
			if s.Operator == "of" {
				e := objectExpr
				if e.Kind != "call_expression" || e.Object == nil || e.Object.Kind != "member_expression" || e.Object.Object == nil || e.Object.Object.Kind != "identifier" || e.Object.Object.Name != "Object" || (e.Object.Name != "keys" && e.Object.Name != "values") || len(e.Args) != 1 {
					return nil, false, false
				}
				if _, _, shadow, _ := ev.binding(c, "Object"); shadow {
					return nil, false, false
				}
				if _, _, shadow := c.env.get("Object"); shadow {
					return nil, false, false
				}
				valuesMode = e.Object.Name == "values"
				objectExpr = e.Args[0]
			} else if s.Operator != "in" {
				return nil, false, false
			}
			objects := ev.expression(c, objectExpr)
			if len(objects) != 1 || objects[0].kind != "object" || !objects[0].literalObject {
				return nil, false, false
			}
			o := objects[0]
			for _, key := range ownKeys(o) {
				if !ev.step() {
					return nil, false, false
				}
				value := stringValue(key)
				if p := o.properties[key]; p.source != nil {
					ev.addEvidence(p.source.File, &p.source.Span)
				}
				if valuesMode {
					value = o.properties[key]
				}
				child := c
				child.env = &environment{parent: c.env, values: map[string]localValue{s.Name: {values: valueSet{value}}}}
				v, returned, ok := ev.statements(child, s.Body)
				if !ok || returned {
					return v, returned, ok
				}
			}
		default:
			return nil, false, false
		}
	}
	return nil, false, true
}

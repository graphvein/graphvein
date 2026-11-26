package routes

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// This file reads an Axum handler's responses (rows SH-X21..SH-X27 of
// docs/contract/claims/route-shape-axum.md): the declared return type,
// and for `impl IntoResponse` the returned expressions, followed through
// proven calls.

// responses reads the handler's responses and follows returned calls.
func (b *shapeBuild) responses(f *axumFn) {
	r := &responseResolver{b: b}
	start := routeshape.Frame{Function: b.s.step(f.fn.Identity), Value: "return"}
	res, err := routeshape.Follow(context.Background(), start, b.s.depth, r)
	if err != nil {
		return
	}
	for _, c := range res.Chains {
		switch c.Stop {
		case routeshape.StopEnded, routeshape.StopJoined:
		default:
			b.reason(routeshape.SectionResponses, chainReason(c))
		}
	}
	if len(res.Chains) > 1 || len(res.Chains) == 1 && res.Chains[0].Depth > 0 {
		b.sh.Follow.Chains = append(b.sh.Follow.Chains, routeshape.Records(routeshape.SectionResponses, res)...)
	}
}

type responseResolver struct {
	b *shapeBuild
}

// respCtx is one function being read for responses.
type respCtx struct {
	b     *shapeBuild
	f     *axumFn
	chain []routeshape.Step
	calls []routeshape.Call
}

func (r *responseResolver) Visit(ctx context.Context, frame routeshape.Frame) ([]routeshape.Call, error) {
	id, ok := r.b.s.steps[axumStepKey(frame.Function)]
	if !ok {
		return nil, fmt.Errorf("function %s not read", frame.Function.Function)
	}
	c := &respCtx{b: r.b, f: r.b.s.fns[id], chain: frame.Chain}
	c.function()
	return c.calls, nil
}

func (c *respCtx) add(status string, contentType *string, schema *routeshape.Schema, rule string, line int) {
	if emptyBody(status) {
		contentType, schema = nil, &routeshape.Schema{Type: routeshape.TypeNull}
	}
	sec := &c.b.sh.Responses
	for i, it := range sec.Items {
		same := it.Status == status && (it.ContentType == nil) == (contentType == nil) && (contentType == nil || *it.ContentType == *contentType)
		if !same {
			continue
		}
		if !reflect.DeepEqual(routeshape.WithoutDecls(it.Schema), routeshape.WithoutDecls(schema)) {
			// Two values answer with the same status and content type but
			// different (or unread) schemas: which one is sent is not read
			// (row SH-X27).
			u := unknownSchema("values with different schemas answer " + status)
			routeshape.AddDecls(&u, it.Schema)
			routeshape.AddDecls(&u, schema)
			sec.Items[i].Schema = &u
		} else {
			routeshape.AddDecls(sec.Items[i].Schema, schema)
		}
		return
	}
	sec.Items = append(sec.Items, routeshape.Response{Status: status, ContentType: contentType, Schema: schema, Provenance: axumProv(routeshape.Returned, rule, c.f.file, line, c.chain)})
}

func (c *respCtx) reason(text string) {
	c.b.reason(routeshape.SectionResponses, text)
}

func (c *respCtx) ser(t *tref) *routeshape.Schema {
	sch := c.b.s.schema(t, &schemaCtx{view: viewSer, visit: map[string]bool{}})
	return &sch
}

func (c *respCtx) resolve(t facts.RustType) *tref {
	return c.b.s.resolveType(t, c.f.file, c.f.fn.Scope, c.generics(), 0)
}

// generics maps the function's own type parameters to "not instantiated".
func (c *respCtx) generics() map[string]*tref {
	g := map[string]*tref{}
	for _, n := range c.f.fn.Generics {
		g[n] = nil
	}
	return g
}

// function reads one function's declared return type and its results.
func (c *respCtx) function() {
	fn := c.f.fn
	var ret *tref
	if fn.Return == nil {
		ret = &tref{kind: "unit", text: "()"}
	} else {
		ret = c.resolve(*fn.Return)
	}
	c.typed(ret, fn.Results, fn.Line)
}

// typed reads the values of a function returning t (SH-X21..SH-X27).
func (c *respCtx) typed(t *tref, values []facts.RustValue, line int) {
	switch {
	case t.std("Result") && len(t.args) == 2:
		var oks []facts.RustValue
		unread := 0
		for _, v := range values {
			switch {
			case v.Kind == "call" && v.Name == "Ok" && len(v.Args) == 1:
				oks = append(oks, v.Args[0])
			case v.Kind == "call" && v.Name == "Err" && len(v.Args) == 1:
				c.errorValue(t.args[1], v.Args[0])
			default:
				unread++
				c.reason(fmt.Sprintf("result value %s at %s:%d not read", valueText(v), c.f.file, v.Line))
			}
		}
		if c.f.fn.Try {
			c.reason(fmt.Sprintf("errors returned by ? in %s are not read", c.f.fn.Identity))
		}
		if len(oks) == 0 && unread == 0 {
			// Every value is an Err: the Ok type is never answered.
			return
		}
		c.typed(t.args[0], oks, line)
	case isAxum(t, "Json"):
		c.add("200", strp("application/json"), c.ser(t.arg(0)), axumRowJSONResponse, line)
	case t.kind == "tuple" && len(t.args) >= 2 && isStatusType(t.args[0]):
		for _, v := range values {
			status, ok := "", false
			if v.Kind == "tuple" && len(v.Args) == len(t.args) {
				status, ok = c.b.s.status(c.f.file, c.f.fn.Scope, v.Args[0])
			}
			if !ok {
				c.reason(fmt.Sprintf("status of %s at %s:%d not read", valueText(v), c.f.file, v.Line))
				continue
			}
			ct, sch := c.bodyOfType(t.args[len(t.args)-1])
			ct = c.partsContentType(v.Args[1:len(v.Args)-1], ct)
			c.add(status, ct, sch, axumRowStatusTuple, v.Line)
		}
	case isStatusType(t):
		for _, v := range values {
			status, ok := c.b.s.status(c.f.file, c.f.fn.Scope, v)
			if !ok {
				c.reason(fmt.Sprintf("status %s at %s:%d not read", valueText(v), c.f.file, v.Line))
				continue
			}
			c.add(status, nil, &routeshape.Schema{Type: routeshape.TypeNull}, axumRowStatus, v.Line)
		}
	case isAxum(t, "Redirect"):
		for _, v := range values {
			c.redirect(v)
		}
	case t.std("String str") || t.kind == "unit" || isAxum(t, "Html"):
		ct, sch := c.bodyOfType(t)
		c.add("200", ct, sch, axumRowText, line)
	case t.kind == "impl" && t.name == "IntoResponse" || isAxum(t, "Response"):
		for _, v := range values {
			c.value(v)
		}
	default:
		c.reason(fmt.Sprintf("return type %s of %s not read", t.text, c.f.fn.Identity))
	}
}

func isAxum(t *tref, name string) bool {
	return t != nil && t.kind == "ext" && t.name == name && facts.Builtin(t.crate, "axum axum_core")
}

func isStatusType(t *tref) bool {
	return t != nil && t.kind == "ext" && t.name == "StatusCode" && facts.Builtin(t.crate, "axum http reqwest hyper")
}

func valueText(v facts.RustValue) string {
	switch v.Kind {
	case "path", "call", "struct", "macro":
		return v.Name
	case "method":
		return "." + v.Name + "()"
	case "literal":
		return fmt.Sprintf("%q", v.Name)
	}
	if v.Text != "" {
		return v.Text
	}
	return v.Kind
}

// bodyOfType is the content type and schema of a response body type.
func (c *respCtx) bodyOfType(t *tref) (*string, *routeshape.Schema) {
	switch {
	case isAxum(t, "Json"):
		return strp("application/json"), c.ser(t.arg(0))
	case t.std("String str"):
		return c.textType(false), &routeshape.Schema{Type: routeshape.TypeString}
	case isAxum(t, "Html"):
		return c.textType(true), &routeshape.Schema{Type: routeshape.TypeString}
	case t.kind == "unit":
		return nil, &routeshape.Schema{Type: routeshape.TypeNull}
	}
	return nil, nil
}

// textType is the content type axum gives a text body (row SH-X25):
// `text/plain` or `text/html` in axum 0.3, with `; charset=utf-8` since
// 0.4 (axum-core 0.1). It is not claimed (nil) while the crate's axum
// version is not proven (rows SH-X7, SH-X11, SH-X25).
func (c *respCtx) textType(html bool) *string {
	base := "text/plain"
	if html {
		base = "text/html"
	}
	v := c.b.s.versions.of(c.f.file)
	switch {
	case v.atLeast(axum04):
		return strp(base + "; charset=utf-8")
	case v.atLeast(semver{0, 3, 0}) && v.below(axum04):
		return strp(base)
	}
	c.reason("the content type of text responses depends on the axum version, which is not proven (" + v.why + ")")
	return nil
}

// partsContentType reads the response parts between the status and the
// body of a returned tuple (row SH-X22): they are applied after the body,
// so a content-type header among them replaces the body's. Arrays of
// (header name, value) pairs are read; the body's content type stays
// when none names content-type, a literal value replaces it, and any
// other part leaves it not read (nil).
func (c *respCtx) partsContentType(parts []facts.RustValue, body *string) *string {
	ct := body
	for _, p := range parts {
		if p.Kind != "array" {
			return nil
		}
		for _, pair := range p.Args {
			if pair.Kind != "tuple" || len(pair.Args) != 2 {
				return nil
			}
			name, ok := c.b.s.headerName(c.f.file, c.f.fn.Scope, pair.Args[0])
			if !ok {
				return nil
			}
			if !strings.EqualFold(name, "content-type") {
				continue
			}
			if pair.Args[1].Kind != "literal" {
				return nil
			}
			ct = strp(pair.Args[1].Name)
		}
	}
	return ct
}

// redirect reads Redirect::temporary and Redirect::permanent (SH-X24).
// Redirect::to answers 303 since axum 0.5 and 307 before, and the
// version is not read: its status is not claimed.
func (c *respCtx) redirect(v facts.RustValue) {
	if v.Kind == "call" {
		b := c.b.s.modules.Resolve(c.f.file, c.f.fn.Scope, v.Name)
		if !b.Ambiguous && facts.Builtin(b.External, "axum") {
			switch lastSegment(b.Name) {
			case "temporary":
				c.add("307", nil, &routeshape.Schema{Type: routeshape.TypeNull}, axumRowRedirect, v.Line)
				return
			case "permanent":
				c.add("308", nil, &routeshape.Schema{Type: routeshape.TypeNull}, axumRowRedirect, v.Line)
				return
			}
		}
	}
	c.reason(fmt.Sprintf("redirect status of %s at %s:%d not read", valueText(v), c.f.file, v.Line))
}

// value reads one returned expression of an `impl IntoResponse` function
// (SH-X27).
func (c *respCtx) value(v facts.RustValue) {
	if v.Kind == "method" && v.Name == "into_response" && v.Recv != nil && len(v.Args) == 0 {
		inner := *v.Recv
		if inner.Line == 0 {
			inner.Line = v.Line
		}
		v = inner
	}
	if v.Try {
		c.reason(fmt.Sprintf("value %s at %s:%d not read", valueText(v), c.f.file, v.Line))
		return
	}
	switch v.Kind {
	case "literal":
		c.add("200", c.textType(false), &routeshape.Schema{Type: routeshape.TypeString}, axumRowText, v.Line)
		return
	case "path":
		if status, ok := c.b.s.status(c.f.file, c.f.fn.Scope, v); ok {
			c.add(status, nil, &routeshape.Schema{Type: routeshape.TypeNull}, axumRowStatus, v.Line)
			return
		}
	case "tuple":
		if len(v.Args) >= 2 {
			if status, ok := c.b.s.status(c.f.file, c.f.fn.Scope, v.Args[0]); ok {
				ct, sch := c.bodyOfValue(v.Args[len(v.Args)-1])
				ct = c.partsContentType(v.Args[1:len(v.Args)-1], ct)
				c.add(status, ct, sch, axumRowStatusTuple, v.Line)
				return
			}
		}
	case "call":
		if c.isAxumCall(v, "Json") && len(v.Args) == 1 {
			ct, sch := c.bodyOfValue(v)
			c.add("200", ct, sch, axumRowJSONResponse, v.Line)
			return
		}
		b := c.b.s.modules.Resolve(c.f.file, c.f.fn.Scope, v.Name)
		if !b.Ambiguous && b.External == "axum" && strings.Contains(b.Name, "Redirect::") {
			c.redirect(v)
			return
		}
		site := routeshape.Step{File: c.f.file, Line: v.Line, Function: v.Name}
		id, stop, detail := c.b.s.target(c.f, v)
		if stop != "" {
			c.calls = append(c.calls, routeshape.Call{Site: site, Stop: stop, Detail: detail})
			return
		}
		c.calls = append(c.calls, routeshape.Call{Site: site, Target: c.b.s.step(id), Value: "return"})
		return
	}
	c.reason(fmt.Sprintf("returned value %s at %s:%d not read", valueText(v), c.f.file, v.Line))
}

func (c *respCtx) isAxumCall(v facts.RustValue, name string) bool {
	if v.Kind != "call" {
		return false
	}
	b := c.b.s.modules.Resolve(c.f.file, c.f.fn.Scope, v.Name)
	return !b.Ambiguous && facts.Builtin(b.External, "axum axum_core") && lastSegment(b.Name) == name
}

// bodyOfValue is the content type and schema of a response body value:
// Json(x) with x's type read (SH-X21), a string literal.
func (c *respCtx) bodyOfValue(v facts.RustValue) (*string, *routeshape.Schema) {
	switch {
	case c.isAxumCall(v, "Json") && len(v.Args) == 1:
		t := c.valueType(v.Args[0], 0)
		if t == nil {
			return strp("application/json"), nil
		}
		return strp("application/json"), c.ser(t)
	case v.Kind == "literal":
		return c.textType(false), &routeshape.Schema{Type: routeshape.TypeString}
	}
	return nil, nil
}

// valueType is the declared type of a value, when it is proven: a struct
// literal of a declaration without generics, a name bound once to such a
// value or with a type, or a parameter's type.
func (c *respCtx) valueType(v facts.RustValue, depth int) *tref {
	if depth > 4 || v.Try {
		return nil
	}
	fn := c.f.fn
	switch v.Kind {
	case "struct":
		t := c.b.s.resolveType(facts.RustType{Kind: "path", Name: v.Name, Text: v.Text}, c.f.file, fn.Scope, c.generics(), 0)
		if t.kind != "local" || len(t.decl.decl.Generics) > 0 || t.decl.decl.Kind != "struct" {
			return nil
		}
		return t
	case "path":
		if strings.Contains(v.Name, "::") {
			return nil
		}
		var found []facts.RustLet
		for _, l := range fn.Lets {
			if l.Name == v.Name {
				found = append(found, l)
			}
		}
		params := 0
		var param facts.RustParam
		for _, p := range fn.Params {
			if p.Name == v.Name || p.Inner == v.Name {
				params++
				param = p
			}
		}
		// Every binding of the name (match arms, if let, for, closures,
		// lets of any pattern): typed only when there is one (row SH-X21).
		bindings := 0
		for _, b := range fn.Bindings {
			if b == v.Name {
				bindings++
			}
		}
		switch {
		case len(found) == 1 && params == 0 && bindings == 1:
			if found[0].Type != nil {
				return c.resolve(*found[0].Type)
			}
			return c.valueType(found[0].Value, depth+1)
		case len(found) == 0 && params == 1 && bindings == 0 && param.Name == v.Name:
			h := c.b.s.classify(c.f.file, fn.Scope, param)
			if h.role != roleUnknown && h.role != roleText {
				return nil
			}
			return c.resolve(param.Type)
		}
	}
	return nil
}

// errorValue reads `Err(E::Variant ..)` with E's IntoResponse impl
// (SH-X26): the status of the variant when the impl proves it.
func (c *respCtx) errorValue(e *tref, v facts.RustValue) {
	variant := ""
	switch v.Kind {
	case "path", "call", "struct":
		variant = lastSegment(v.Name)
	}
	if e == nil || e.kind != "local" || e.decl.decl.Kind != "enum" || variant == "" || v.Try {
		c.reason(fmt.Sprintf("error %s at %s:%d not read", valueText(v), c.f.file, v.Line))
		return
	}
	statuses, ok := c.b.s.variantStatuses(e.decl)
	status, known := statuses[variant]
	if !ok || !known {
		c.reason(fmt.Sprintf("status of error %s at %s:%d not read", valueText(v), c.f.file, v.Line))
		return
	}
	c.add(status, nil, nil, axumRowError, v.Line)
}

// variantStatuses maps the variants of an error enum to the status its
// IntoResponse impl answers with (row SH-X26). Every result of
// into_response (and of a `self.m()` method it calls for the status) is
// read with the match arms on its path: a match on `self` narrows the
// variants a result can answer for (an arm naming variants, a wildcard
// the variants no earlier arm names), and other matches and `if`
// branches only add results. A variant is proven when every result that
// can answer for it is the same StatusCode constant (alone or first in a
// tuple) and no arm on `self` that can match it has a guard; any other
// result makes the variants it can answer for unknown. ok is false when
// the enum has no single IntoResponse impl.
func (s *axumShaper) variantStatuses(d *axumTypeDecl) (map[string]string, bool) {
	var impl *axumFn
	for _, id := range s.traitFns {
		f := s.fns[id]
		if lastSegment(f.fn.Trait) != "IntoResponse" {
			continue
		}
		self := f.fn.SelfType
		b := s.modules.Resolve(f.file, f.fn.Scope, self)
		if b.Identity == d.decl.Identity && !b.Ambiguous && strings.Contains(id, "into_response") {
			if impl != nil {
				return nil, false
			}
			impl = f
		}
	}
	if impl == nil {
		return nil, false
	}
	var all []string
	variants := map[string]bool{}
	for _, v := range d.decl.Variants {
		all = append(all, v.Name)
		variants[v.Name] = true
	}
	statuses := map[string]map[string]bool{}
	unknown := map[string]bool{}
	var walk func(f *axumFn, values []facts.RustValue, outer map[string]bool, depth int)
	walk = func(f *axumFn, values []facts.RustValue, outer map[string]bool, depth int) {
		for _, v := range values {
			cover, guarded := armCoverage(f, v.Arms, d.decl.Name, all, variants)
			for n := range cover {
				if !outer[n] {
					delete(cover, n)
				}
			}
			for n := range guarded {
				if outer[n] {
					unknown[n] = true
				}
			}
			if v.Kind == "method" && v.Name == "into_response" && v.Recv != nil && len(v.Args) == 0 {
				v = *v.Recv
			}
			if v.Kind == "tuple" && len(v.Args) >= 2 {
				v = v.Args[0]
			}
			if v.Kind == "method" && v.Recv != nil && v.Recv.Kind == "path" && v.Recv.Name == "self" && len(v.Args) == 0 && depth == 0 {
				if id, stop, _ := s.target(f, v); stop == "" {
					walk(s.fns[id], s.fns[id].fn.Results, cover, depth+1)
					continue
				}
			}
			status, ok := s.status(f.file, f.fn.Scope, v)
			for n := range cover {
				if !ok {
					unknown[n] = true
					continue
				}
				if statuses[n] == nil {
					statuses[n] = map[string]bool{}
				}
				statuses[n][status] = true
			}
		}
	}
	outer := map[string]bool{}
	for _, n := range all {
		outer[n] = true
	}
	walk(impl, impl.fn.Results, outer, 0)
	out := map[string]string{}
	for _, n := range all {
		if unknown[n] || len(statuses[n]) != 1 {
			continue
		}
		for st := range statuses[n] {
			out[n] = st
		}
	}
	return out, true
}

// armCoverage returns the variants a result reached through arms can
// answer for, and the variants a guarded arm on its path can match. Only
// matches on `self` narrow; an arm pattern that is not read, a binding
// or `_` covers every variant no earlier arm of the same match names.
func armCoverage(f *axumFn, arms []facts.RustArm, typeName string, all []string, variants map[string]bool) (map[string]bool, map[string]bool) {
	cover := map[string]bool{}
	for _, n := range all {
		cover[n] = true
	}
	guarded := map[string]bool{}
	for _, arm := range arms {
		switch arm.Scrutinee {
		case "self", "*self", "&self":
		default:
			continue
		}
		named := armVariants(arm.Pattern, typeName, variants)
		this := map[string]bool{}
		if len(named) == 0 || contains(named, "_") {
			// A wildcard: every variant no earlier, unguarded arm of
			// this match names (an earlier arm with no recorded result
			// is not known, which only widens this).
			earlier := map[string]bool{}
			for _, r := range f.fn.Results {
				for _, a := range r.Arms {
					if a.Match == arm.Match && a.Index < arm.Index && !a.Guard {
						for _, n := range armVariants(a.Pattern, typeName, variants) {
							if n != "_" {
								earlier[n] = true
							}
						}
					}
				}
			}
			for _, n := range all {
				if !earlier[n] {
					this[n] = true
				}
			}
		} else {
			for _, n := range named {
				this[n] = true
			}
		}
		for n := range cover {
			if !this[n] {
				delete(cover, n)
			}
		}
		if arm.Guard {
			for n := range this {
				guarded[n] = true
			}
		}
	}
	return cover, guarded
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// armVariants reads the variants a match arm pattern names: `E::V`,
// `Self::V`, `V`, with `(..)`, `{..}` and `|` alternatives; `_` is the
// wildcard. nil when the pattern is not read.
func armVariants(pattern, typeName string, variants map[string]bool) []string {
	if pattern == "" {
		return nil
	}
	var out []string
	for _, alt := range strings.Split(pattern, "|") {
		alt = strings.TrimSpace(alt)
		if alt == "_" {
			out = append(out, "_")
			continue
		}
		if i := strings.IndexAny(alt, "({"); i >= 0 {
			alt = alt[:i]
		}
		parts := strings.Split(alt, "::")
		name := parts[len(parts)-1]
		if len(parts) == 2 && parts[0] != "Self" && parts[0] != typeName || len(parts) > 2 || !variants[name] {
			return nil
		}
		out = append(out, name)
	}
	return out
}

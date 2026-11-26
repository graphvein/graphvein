package routes

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/python"
)

// This file turns what the follow collected into the route's sections
// (rows SH-F2, SH-F3, SH-F18..SH-F30) and reads the security schemes and
// requirement from the security chain facts (rows SH-F32, SH-F33).

func jsonText(v any) (string, error) {
	data, err := json.Marshal(v)
	return string(data), err
}

func strPtr(s string) *string { return &s }

// registrationProv is a fact at the route registration.
func (r *routeBuild) registrationProv(kind routeshape.Kind, rule string) routeshape.Provenance {
	return routeshape.Provenance{Kind: kind, Rule: rule, File: r.in.file, Line: r.in.syntax.Call.Span.StartLine, Chain: []routeshape.Step{}, Confidence: 1}
}

// handlerProv is a fact at the handler definition.
func (r *routeBuild) handlerProv(rule string) *routeshape.Provenance {
	p := routeshape.Provenance{Kind: routeshape.Fact, Rule: rule, File: r.handler.file, Line: r.handler.fn.Line, Chain: []routeshape.Step{}, Confidence: 1}
	return &p
}

// convertorSchema is the schema of a Starlette path convertor (row SH-F2).
func convertorSchema(c string) (routeshape.Schema, bool) {
	switch c {
	case "", "str", "path", "uuid":
		return routeshape.Schema{Type: routeshape.TypeString}, true
	case "int":
		return routeshape.Schema{Type: routeshape.TypeInteger}, true
	case "float":
		return routeshape.Schema{Type: routeshape.TypeNumber}, true
	}
	return routeshape.Schema{}, false
}

func (r *routeBuild) assemble() {
	sh := r.shape
	r.assemblePath()
	for i, section := range []string{routeshape.SectionQuery, routeshape.SectionHeader, routeshape.SectionCookie} {
		target := []*routeshape.ParamSection{&sh.Query, &sh.Header, &sh.Cookie}[i]
		target.Items = r.params[section]
		r.state(&target.Section, section, kinds(target.Items))
	}
	r.assembleBody()
	r.assembleResponses()
	sh.Models = append(sh.Models, r.l.types.routeModels(r.refs)...)
}

func kinds(ps []routeshape.Param) []routeshape.Kind {
	var out []routeshape.Kind
	for _, p := range ps {
		out = append(out, p.Provenance.Kind)
	}
	return out
}

// state sets a parameter or body section's state (row SH-F23): complete
// or provably_none only when nothing may be missing.
func (r *routeBuild) state(sec *routeshape.Section, name string, ks []routeshape.Kind) {
	reasons := r.reasons[name]
	if len(reasons) > 0 {
		sec.State, sec.Reason = routeshape.Partial, strings.Join(reasons, "; ")
		return
	}
	for _, k := range ks {
		if k != routeshape.Fact {
			sec.State, sec.Reason = routeshape.Partial, "names read in code"
			return
		}
	}
	tagShape("SH-F23")
	sec.Evidence = r.handlerProv("SH-F23")
	if len(ks) == 0 {
		tagShape("SH-F23/provably_none")
		sec.State = routeshape.ProvablyNone
		return
	}
	tagShape("SH-F23/complete")
	sec.State = routeshape.Complete
}

// assemblePath builds the path section from the template parameters and
// the declared and read path parameters (rows SH-F1..SH-F3).
func (r *routeBuild) assemblePath() {
	sec := &r.shape.Path
	params, order, ok := r.templateParams()
	if len(r.in.templates) == 0 {
		return // not_read: the route is not mounted.
	}
	declared := map[string]routeshape.Param{}
	var read []routeshape.Param
	for _, p := range r.params[routeshape.SectionPath] {
		if p.Access == routeshape.Declared {
			declared[p.Name] = p
		} else {
			read = append(read, p)
		}
	}
	var reasons []string
	if !ok {
		reasons = append(reasons, "the route is mounted under prefixes with different path parameters")
		for _, p := range r.params[routeshape.SectionPath] {
			sec.Items = append(sec.Items, p)
		}
	} else {
		incomplete := len(r.reasons[routeshape.SectionQuery]) > 0
		for _, name := range order {
			if p, found := declared[name]; found {
				sec.Items = append(sec.Items, p)
				if p.Schema == nil {
					reasons = append(reasons, "path parameter "+name+": type not read")
				}
				continue
			}
			tagShape("SH-F2")
			p := routeshape.Param{Name: name, Access: routeshape.Declared, Presence: routeshape.Required, Provenance: r.registrationProv(routeshape.Fact, "SH-F2")}
			if s, known := convertorSchema(params[name]); known && !incomplete {
				p.Schema = &s
			} else {
				reasons = append(reasons, "path parameter "+name+": type not read")
			}
			sec.Items = append(sec.Items, p)
		}
		sec.Items = append(sec.Items, read...)
	}
	if len(read) > 0 {
		reasons = append(reasons, "names read in code")
	}
	if len(reasons) > 0 {
		sec.State, sec.Reason = routeshape.Partial, strings.Join(reasons, "; ")
		return
	}
	tagShape("SH-F3")
	ev := r.registrationProv(routeshape.Fact, "SH-F3")
	sec.Evidence = &ev
	if len(sec.Items) == 0 {
		tagShape("SH-F3/provably_none")
		sec.State = routeshape.ProvablyNone
		return
	}
	tagShape("SH-F3/complete")
	sec.State = routeshape.Complete
}

// assembleBody builds the request body as FastAPI's get_body_field does
// (rows SH-F18..SH-F21).
func (r *routeBuild) assembleBody() {
	sec := &r.shape.Body
	var ks []routeshape.Kind
	// FastAPI keeps one body parameter per name (a dependency declaring
	// the handler's body parameter again adds nothing).
	var unique []bodyParam
	for _, b := range r.body {
		dup := false
		for _, u := range unique {
			if u.name == b.name {
				dup = true
				if !sameSchema(&u.schema, &b.schema) || u.presence != b.presence || u.marker != b.marker {
					r.reason(routeshape.SectionBody, "body parameter "+b.name+" is declared twice with different types")
				}
			}
		}
		if !dup {
			unique = append(unique, b)
		}
	}
	r.body = unique
	formModel := false
	for _, b := range r.body {
		formModel = formModel || b.formModel
	}
	switch {
	case formModel && len(r.body) > 1:
		r.reason(routeshape.SectionBody, "a form model with other body parameters is not read")
	case formModel:
		// FastAPI reads the model's fields as the form fields: the body
		// schema is the model's (row SH-F20).
		s := r.body[0].schema
		c := routeshape.Content{ContentType: strPtr("application/x-www-form-urlencoded"), Schema: &s, Provenance: r.body[0].prov}
		sec.Items = append(sec.Items, c)
		ks = append(ks, c.Provenance.Kind)
	case len(r.body) > 0:
		form, file, mixed := false, false, false
		for _, b := range r.body {
			switch b.marker {
			case "Form":
				form = true
			case "File":
				form, file = true, true
			}
			if b.file {
				form, file = true, true
			}
		}
		for _, b := range r.body {
			if form && b.marker != "Form" && b.marker != "File" {
				mixed = true
			}
		}
		if mixed {
			r.reason(routeshape.SectionBody, "form fields mixed with JSON body parameters are not read")
		} else {
			first := r.body[0]
			c := routeshape.Content{Provenance: first.prov}
			if len(r.body) == 1 && !first.embed && !form {
				s := first.schema
				c.Schema = &s
			} else {
				rule := "SH-F19"
				if form {
					rule = "SH-F20"
				}
				if len(r.body) > 1 {
					tagShape("SH-F19/several")
				}
				obj := routeshape.Schema{Type: routeshape.TypeObject, Additional: routeshape.AdditionalUnknown}
				seen := map[string]bool{}
				for _, b := range r.body {
					if seen[b.name] {
						continue
					}
					seen[b.name] = true
					obj.Fields = append(obj.Fields, routeshape.Field{Name: b.name, Presence: b.presence, Schema: b.schema})
				}
				c.Schema = &obj
				c.Provenance.Rule = rule
				tagShape(rule)
			}
			ct := "application/json"
			switch {
			case file:
				ct = "multipart/form-data"
				tagShape("SH-F20/multipart")
			case form:
				ct = "application/x-www-form-urlencoded"
			default:
				for _, b := range r.body {
					if b.mediaType != "" {
						ct = b.mediaType
						break
					}
				}
			}
			c.ContentType = strPtr(ct)
			sec.Items = append(sec.Items, c)
			ks = append(ks, c.Provenance.Kind)
		}
	}
	for _, c := range r.rawBody {
		sec.Items = append(sec.Items, c)
		ks = append(ks, c.Provenance.Kind)
	}
	r.state(&sec.Section, routeshape.SectionBody, ks)
}

// noBody reports a status FastAPI sends without a body.
func noBody(status string) bool {
	n := statusInt(status)
	return n < 200 || n == 204 || n == 304
}

// assembleResponses builds the responses section (rows SH-F24..SH-F31,
// SH-F36, SH-F37, SH-F39).
func (r *routeBuild) assembleResponses() {
	sec := &r.shape.Responses
	e := r.in.syntax.Expression
	scope := r.in.syntax.Call.Scope
	var out []routeshape.Response
	add := func(resp routeshape.Response) {
		for _, o := range out {
			if o.Status == resp.Status && o.Provenance.Kind == resp.Provenance.Kind && ((o.ContentType == nil) == (resp.ContentType == nil)) && (o.ContentType == nil || *o.ContentType == *resp.ContentType) {
				return
			}
		}
		out = append(out, resp)
	}
	unknown := func(reason string) *routeshape.Schema {
		return &routeshape.Schema{Type: routeshape.TypeUnknown, Reason: reason}
	}
	status := "200"
	statusKnown, explicit := true, false
	if v, ok := keyword(e, "status_code"); ok {
		tagShape("SH-F24/status_code")
		explicit = true
		status, statusKnown = r.statusCode(r.in.file, scope, v)
		if !statusKnown {
			r.reason(routeshape.SectionResponses, "status_code is not read")
		}
	} else {
		tagShape("SH-F24/default")
	}
	var ct *string
	if v, ok := keyword(e, "response_class"); ok {
		tagShape("SH-F24/response_class")
		b := r.l.modules.Expression(r.in.file, scope, v)
		if t, known := responseClass(b); known && t != "" {
			ct = strPtr(t)
		}
		if !explicit {
			// FastAPI passes no status: the class's own default applies.
			if st, known := responseClassStatus(b); known {
				if st != "200" {
					tagShape("SH-F24/class default")
				}
				status = st
			} else {
				statusKnown = false
				r.reason(routeshape.SectionResponses, "the default status of response_class "+describe(v)+" is not read")
			}
		}
	} else {
		if len(r.l.responseClassSet) > 0 {
			tagShape("SH-F24/x.default_response_class = C")
		}
		if len(r.in.responseClass)+len(r.l.responseClassSet) == 0 {
			ct = strPtr("application/json")
		}
		if statusWhy := append(append([]string{}, r.in.responseStatus...), r.l.responseClassSet...); !explicit && len(statusWhy) > 0 {
			statusKnown = false
			for _, why := range statusWhy {
				r.reason(routeshape.SectionResponses, why+": the default success status is not read")
			}
		}
	}
	stream := r.handler.fn.Generator
	var annotation *typeInfo
	var annotationRefs []string
	if r.handler.fn.Returns != nil {
		info := r.l.types.resolve(r.handler.file, r.enclosing(r.handler), *r.handler.fn.Returns, 0, &annotationRefs)
		annotation = &info
		stream = stream || info.stream
	}
	options := r.responseOptions()
	successAt := -1
	if statusKnown {
		success := routeshape.Response{Status: status, ContentType: ct, Provenance: r.registrationProv(routeshape.Fact, "SH-F24")}
		model, hasModel := keyword(e, "response_model")
		withOptions := func(s *routeshape.Schema) *routeshape.Schema {
			if options != "" && s.Type != routeshape.TypeUnknown {
				tagShape("SH-F36")
				return unknown(options + ": the response may leave out or rename fields")
			}
			return s
		}
		switch {
		case noBody(status):
			tagShape("SH-F24/no-body")
			success.ContentType = nil
			success.Schema = &routeshape.Schema{Type: routeshape.TypeNull}
		case stream:
			tagShape("SH-F39")
			success.Provenance.Kind, success.Provenance.Rule = routeshape.Intended, "SH-F39"
			success.ContentType = nil
			success.Schema = unknown("the handler streams its values (a generator or an iterable return annotation): the content type depends on the FastAPI version")
			r.reason(routeshape.SectionResponses, "the handler is a generator or returns an iterable: FastAPI streams it (JSON Lines in recent versions)")
		case hasModel && model.Kind == "none":
			tagShape("SH-F27/response_model=None")
			success.Provenance.Rule = "SH-F27"
			success.Schema = unknown("response_model=None: the response is not validated")
		case hasModel:
			tagShape("SH-F25")
			info := r.l.types.resolve(r.in.file, scope, model, 0, &r.refs)
			success.Provenance.Rule = "SH-F25"
			success.Schema = withOptions(&info.schema)
			if info.cat == catSpecial {
				success.Schema = unknown("response_model is not a model")
			}
		case annotation != nil:
			switch annotation.cat {
			case catNone:
				tagShape("SH-F27/-> None")
				success.Provenance.Rule = "SH-F27"
				success.Schema = unknown("-> None: no response model")
			case catSpecial:
				tagShape("SH-F27/-> Response")
				success.Provenance.Rule = "SH-F27"
				success.Schema = unknown("the handler returns a Response object")
			default:
				tagShape("SH-F26")
				r.refs = append(r.refs, annotationRefs...)
				kind := routeshape.Returned
				if v, _, ok := r.l.modules.FastAPIMinimum(r.in.file); ok && v.AtLeast(annotationVersion) {
					// FastAPI validates and filters the returned value
					// with the annotation (0.89.0 and later).
					tagShape("SH-F26/validated")
					kind = routeshape.Fact
				} else {
					r.reason(routeshape.SectionResponses, "the return annotation is validated from FastAPI 0.89.0; no pyproject.toml proves that version")
				}
				success.Provenance = routeshape.Provenance{Kind: kind, Rule: "SH-F26", File: r.handler.file, Line: r.handler.fn.Line, Chain: []routeshape.Step{}, Confidence: 1}
				success.Schema = withOptions(&annotation.schema)
			}
		default:
			tagShape("SH-F27/no annotation")
			success.Provenance.Rule = "SH-F27"
			success.Schema = unknown("no response model is declared")
		}
		add(success)
		successAt = 0
		if !noBody(status) && !stream && !hasModel && annotation == nil && len(r.returned) > 0 {
			ret := r.returned[0]
			ret.Status, ret.ContentType = status, ct
			first, _ := jsonText(ret.Schema)
			for _, other := range r.returned[1:] {
				if t, _ := jsonText(other.Schema); t != first {
					ret.Schema = unknown("several returned types")
				}
			}
			add(ret)
		}
	}
	if !stream {
		r.override = append(r.override, r.mayReturnResponse(r.handler, 0, map[string]bool{})...)
	}
	if len(r.override) > 0 && successAt >= 0 && out[successAt].Provenance.Kind != routeshape.Intended {
		// The declared status and model hold only on the other paths.
		tagShape("SH-F24/override")
		out[successAt].Provenance.Kind = routeshape.Intended
		if out[successAt].Schema != nil {
			s := *out[successAt].Schema
			s.Fields = append([]routeshape.Field(nil), s.Fields...)
			unrequire(&s)
			out[successAt].Schema = &s
		}
		for i, o := range r.override {
			if i == 3 {
				r.reason(routeshape.SectionResponses, fmt.Sprintf("and %d more returned values not read", len(r.override)-3))
				break
			}
			r.reason(routeshape.SectionResponses, o)
		}
	}
	r.responsesDict(add)
	if r.declared {
		tagShape("SH-F30")
		kind := routeshape.Fact
		if !r.canFail {
			tagShape("SH-F30/unreachable")
			kind = routeshape.Intended
			r.reason(routeshape.SectionResponses, "422: no declared parameter or body can fail validation (FastAPI documents it, no request reaches it)")
		}
		if len(r.l.handlerValidation) > 0 {
			tagShape("SH-F37")
			kind = routeshape.Intended
			for _, h := range r.l.handlerValidation {
				r.reason(routeshape.SectionResponses, h+" can replace the 422 response")
			}
		}
		add(routeshape.Response{Status: "422", ContentType: strPtr("application/json"), Schema: &routeshape.Schema{Type: routeshape.TypeRef, Ref: "HTTPValidationError"}, Provenance: r.registrationProv(kind, "SH-F30")})
		r.shape.Models = append(r.shape.Models, validationModels(r.registrationProv(routeshape.Fact, "SH-F30"))...)
	}
	for _, resp := range r.responses {
		add(resp)
	}
	sec.Items = out
	reasons := append([]string{"statuses from exception handlers, middleware and calls not followed are not read"}, r.reasons[routeshape.SectionResponses]...)
	sec.State, sec.Reason = routeshape.Partial, strings.Join(reasons, "; ")
}

// annotationVersion is the FastAPI release that validates a handler's
// return annotation as the response model (0.89.0, row SH-F26).
var annotationVersion = python.Version{0, 89, 0}

// responseOptions says why the decorator's response_model_* options may
// leave out or rename fields of the response model (row SH-F36), or "".
// exclude_unset and exclude_defaults drop only fields with a default,
// which are optional in the schema already.
func (r *routeBuild) responseOptions() string {
	e := r.in.syntax.Expression
	var why []string
	for _, k := range []string{"response_model_include", "response_model_exclude", "response_model_exclude_none", "response_model_by_alias"} {
		v, ok := keyword(e, k)
		if !ok {
			continue
		}
		switch {
		case v.Kind == "dictionary_splat":
			return "keyword unpacking may set response_model options"
		case k == "response_model_by_alias" && v.Kind == "true",
			(k == "response_model_include" || k == "response_model_exclude") && v.Kind == "none",
			k == "response_model_exclude_none" && v.Kind == "false":
		default:
			why = append(why, k)
		}
	}
	return strings.Join(why, ", ")
}

// modelConstructors are Pydantic classmethods that return an instance of
// their class.
var modelConstructors = map[string]bool{"model_validate": true, "model_validate_json": true, "model_validate_strings": true, "model_construct": true}

// parameterValue lists why a returned parameter may be a Response: a
// typed parameter is FastAPI's validated value; a dependency parameter
// (Depends/Security) is its callable's returned value.
func (r *routeBuild) parameterValue(sf *shapeFunc, prm facts.PythonShapeParam, at string, depth int, seen map[string]bool) []string {
	file, scope := sf.file, r.enclosing(sf)
	typ, items := r.l.sec.flatten(file, scope, *prm.Annotation, map[string]bool{})
	var marker *metadataItem
	for i := range items {
		switch r.markerOf(items[i].file, items[i].scope, items[i].expr, true) {
		case "Depends", "Security":
			marker = &items[i]
		}
	}
	if marker == nil && prm.Default != nil && prm.Default.Kind == "call" {
		switch r.markerOf(file, scope, *prm.Default, false) {
		case "Depends", "Security":
			marker = &metadataItem{file, scope, *prm.Default}
		}
	}
	if marker == nil {
		if typ != nil && safeType(r.l.types.resolve(typ.file, typ.scope, typ.expr, 0, &[]string{})) {
			return nil
		}
		return []string{fmt.Sprintf("the returned parameter %s is not read (%s): it may be a Response", prm.Name, at)}
	}
	target, ok := argumentOf(marker.expr, "dependency", 0)
	tfile, tscope := marker.file, marker.scope
	if !ok || target.Kind == "none" {
		if typ == nil {
			return []string{fmt.Sprintf("the returned dependency %s is not read (%s): it may be a Response", prm.Name, at)}
		}
		target, tfile, tscope = typ.expr, typ.file, typ.scope
	}
	return r.callableValue(r.l.modules.Expression(tfile, tscope, target), describe(target), at, depth, seen, true)
}

// callableValue lists why the value a resolved callable returns may be a
// Response (mayReturnResponse's rules for one call).
// dependency is true for a Depends() callable: a generator's injected
// value is what it yields, which is not read.
func (r *routeBuild) callableValue(b python.Binding, name, at string, depth int, seen map[string]bool, dependency bool) []string {
	notRead := func(format string, args ...any) []string {
		return []string{fmt.Sprintf(format, args...) + ": it may be a Response"}
	}
	switch {
	case b.External:
		if _, ok := responseClass(b); ok {
			return []string{"a Response is returned directly (" + at + ")"}
		}
		if fullName(b) != "fastapi.encoders.jsonable_encoder" {
			return notRead("the value of %s is returned (%s)", fullName(b), at)
		}
	case b.File == "" || b.Module != nil || b.Syntax.Count != 1:
		return notRead("the value of %s is returned (%s)", name, at)
	case b.Syntax.Value.Kind == "class_definition":
		if root := r.l.types.root(b, map[string]bool{}); root != "model" && root != "enum" {
			return notRead("an instance of %s is returned (%s)", b.Name, at)
		}
	case b.Syntax.Value.Kind == "function_definition":
		target := r.l.byScope[fmt.Sprintf("%s:%d", b.File, b.Syntax.ClassScope)]
		if target == nil || target.method {
			return notRead("the value of %s is returned (%s)", b.Name, at)
		}
		if target.fn.Returns != nil {
			info := r.l.types.resolve(target.file, r.enclosing(target), *target.fn.Returns, 0, &[]string{})
			if info.cat == catSpecial && info.special == "Response" {
				return []string{fmt.Sprintf("%s returns a Response (%s)", b.Name, at)}
			}
			if !safeType(info) {
				return notRead("%s is annotated %s (%s)", b.Name, describe(*target.fn.Returns), at)
			}
			return nil
		}
		if target.fn.Generator {
			if dependency {
				return notRead("the value %s yields is returned (%s)", b.Name, at)
			}
			return nil
		}
		if depth+1 >= r.l.depth {
			return notRead("the value of %s is returned and not followed (depth limit, %s)", b.Name, at)
		}
		return r.mayReturnResponse(target, depth+1, seen)
	default:
		return notRead("the value of %s is returned (%s)", b.Name, at)
	}
	return nil
}

// safeType reports a resolved annotation whose values are never Response
// objects.
func safeType(info typeInfo) bool {
	switch info.cat {
	case catScalar, catSequence, catMapping, catModel, catNone:
		return info.schema.Type != routeshape.TypeUnknown && !info.stream
	}
	return false
}

// mayReturnResponse lists why a function may return a Response object
// (which FastAPI sends as is, bypassing status_code and the response
// model, row SH-F24); empty when every return is proven not to be one: a
// plain value, a typed parameter, a model or enum constructor, or a
// proven function annotated with another type (or, unannotated, whose own
// returns are proven).
func (r *routeBuild) mayReturnResponse(sf *shapeFunc, depth int, seen map[string]bool) []string {
	key := stepKey(sf.step)
	if seen[key] {
		return nil
	}
	seen[key] = true
	p := r.l.files[sf.file].Python
	assigns := map[string][]facts.PythonShapeSite{}
	for _, s := range sf.fn.Sites {
		if s.Kind == "assign" {
			assigns[s.Name] = append(assigns[s.Name], s)
		}
	}
	params := map[string]facts.PythonShapeParam{}
	for _, prm := range sf.fn.Params {
		params[prm.Name] = prm
	}
	var why []string
	notRead := func(format string, args ...any) {
		why = append(why, fmt.Sprintf(format, args...)+": it may be a Response")
	}
	for _, s := range sf.fn.Sites {
		if s.Kind != "return" {
			continue
		}
		at := fmt.Sprintf("%s:%d", sf.file, s.Line)
		if s.Name != "" {
			if prm, ok := params[s.Name]; ok && prm.Annotation != nil && p.Scopes[sf.scope].Bindings[s.Name].Count <= 1 && len(assigns[s.Name]) == 0 {
				why = append(why, r.parameterValue(sf, prm, at, depth, seen)...)
				continue
			}
			if len(assigns[s.Name]) != 1 || p.Scopes[sf.scope].Bindings[s.Name].Count != 1 {
				notRead("the returned value %s is not read (%s)", s.Name, at)
				continue
			}
			s = assigns[s.Name][0]
		}
		if s.Plain {
			continue
		}
		if s.Expr == nil || s.Expr.Object == nil {
			notRead("a returned value is not read (%s)", at)
			continue
		}
		callee := *s.Expr.Object
		if callee.Kind == "attribute" && callee.Object != nil && modelConstructors[callee.Name] {
			// Model.model_validate(...): an instance of a repository model.
			if b := r.l.modules.Expression(sf.file, sf.scope, *callee.Object); !b.External && b.File != "" && b.Module == nil && b.Syntax.Count == 1 &&
				b.Syntax.Value.Kind == "class_definition" && r.l.types.root(b, map[string]bool{}) == "model" {
				continue
			}
		}
		why = append(why, r.callableValue(r.l.modules.Expression(sf.file, sf.scope, callee), describe(callee), at, depth, seen, false)...)
	}
	return why
}

// validationModels are FastAPI's validation error schemas (fastapi
// 0.161.1): HTTPValidationError and ValidationError.
func validationModels(p routeshape.Provenance) []routeshape.Model {
	str := routeshape.Schema{Type: routeshape.TypeString}
	return []routeshape.Model{
		{Name: "HTTPValidationError", Provenance: p, Schema: routeshape.Schema{Type: routeshape.TypeObject, Additional: routeshape.AdditionalUnknown, Fields: []routeshape.Field{
			{Name: "detail", Presence: routeshape.Optional, Schema: routeshape.Schema{Type: routeshape.TypeArray, Items: &routeshape.Schema{Type: routeshape.TypeRef, Ref: "ValidationError"}}},
		}}},
		{Name: "ValidationError", Provenance: p, Schema: routeshape.Schema{Type: routeshape.TypeObject, Additional: routeshape.AdditionalUnknown, Fields: []routeshape.Field{
			{Name: "loc", Presence: routeshape.Required, Schema: routeshape.Schema{Type: routeshape.TypeArray, Items: &routeshape.Schema{Type: routeshape.TypeUnknown, Reason: "a string or an integer"}}},
			{Name: "msg", Presence: routeshape.Required, Schema: str},
			{Name: "type", Presence: routeshape.Required, Schema: str},
			{Name: "input", Presence: routeshape.Optional, Schema: routeshape.Schema{Type: routeshape.TypeUnknown, Reason: "any value"}},
			{Name: "ctx", Presence: routeshape.Optional, Schema: routeshape.Schema{Type: routeshape.TypeObject, Additional: routeshape.AdditionalAllowed}},
		}}},
	}
}

// responsesDict reads a responses= dictionary literal (row SH-F28):
// documented statuses, not checked at runtime (kind intended).
func (r *routeBuild) responsesDict(add func(routeshape.Response)) {
	v, ok := keyword(r.in.syntax.Expression, "responses")
	if !ok {
		return
	}
	scope := r.in.syntax.Call.Scope
	if v.Kind != "dict" || v.Invalid {
		r.reason(routeshape.SectionResponses, "responses= is not a dictionary literal")
		return
	}
	for i, key := range v.Parts {
		status, known := "", false
		switch key.Kind {
		case "literal":
			if statusPatternOK(key.Name) {
				status, known = key.Name, true
			}
		default:
			status, known = r.statusCode(r.in.file, scope, key)
		}
		if !known {
			r.reason(routeshape.SectionResponses, "a responses= key is not read")
			continue
		}
		resp := routeshape.Response{Status: status, Provenance: r.registrationProv(routeshape.Intended, "SH-F28")}
		value := v.Args[i]
		if value.Kind != "dict" || value.Invalid {
			r.reason(routeshape.SectionResponses, "a responses= value is not read")
			continue
		}
		for j, k := range value.Parts {
			if k.Kind == "literal" && k.Name == "model" {
				info := r.l.types.resolve(r.in.file, scope, value.Args[j], 0, &r.refs)
				resp.Schema = &info.schema
				resp.ContentType = strPtr("application/json")
			}
		}
		tagShape("SH-F28")
		add(resp)
	}
}

func statusPatternOK(s string) bool {
	if s == "default" {
		return true
	}
	if len(s) != 3 || s[0] < '1' || s[0] > '5' {
		return false
	}
	if s[1:] == "XX" {
		return true
	}
	n := statusInt(s)
	return n >= 100 && n <= 599
}

// security reads the route's schemes and requirement from the security
// chain facts (rows SH-F32, SH-F33). A scheme is stated only when every
// field its type needs is read; anything else leaves the section partial.
func (r *routeBuild) security() {
	sh := r.shape
	schemes, _ := r.in.security["security_dependencies"].([]securityScheme)
	unknown, _ := r.in.security["security_dependencies_unknown"].([]securityUnknown)
	if len(schemes) == 0 && len(unknown) == 0 {
		return // not_read: no declaration read is no evidence of none.
	}
	var reasons []string
	for _, u := range unknown {
		reasons = append(reasons, "a security dependency is not read: "+u.Reason)
	}
	var uses []routeshape.SchemeUse
	intended := false
	for _, s := range schemes {
		scheme, why := r.l.scheme(s)
		if scheme == nil {
			reasons = append(reasons, why)
			continue
		}
		if s.AutoError != true {
			intended = true
			reasons = append(reasons, fmt.Sprintf("%s: auto_error is not True, so requests without credentials can reach the handler", scheme.Name))
		}
		dup := false
		for i, old := range sh.Schemes {
			if old.Name == scheme.Name {
				dup = true
				a, _ := jsonText(old)
				b, _ := jsonText(*scheme)
				if a != b {
					sh.Schemes = append(sh.Schemes[:i], sh.Schemes[i+1:]...)
					reasons = append(reasons, "two security schemes share the name "+scheme.Name)
				}
				break
			}
		}
		if !dup {
			sh.Schemes = append(sh.Schemes, *scheme)
			uses = append(uses, routeshape.SchemeUse{Name: scheme.Name, Scopes: []string{}})
		}
	}
	if r.scopes {
		reasons = append(reasons, "Security() scopes are not read")
	}
	// Drop uses of schemes removed for a name clash.
	var kept []routeshape.SchemeUse
	for _, u := range uses {
		for _, s := range sh.Schemes {
			if s.Name == u.Name {
				kept = append(kept, u)
			}
		}
	}
	if len(kept) > 0 && !r.scopes {
		kind := routeshape.Fact
		if intended {
			kind = routeshape.Intended
		}
		tagShape("SH-F33")
		sh.Security.Items = []routeshape.Requirement{{Schemes: kept, Provenance: r.registrationProv(kind, "SH-F33")}}
	}
	reasons = append(reasons, "middleware is not read")
	sh.Security.State, sh.Security.Reason = routeshape.Partial, strings.Join(reasons, "; ")
}

// scheme builds one scheme object from a DA-F3 scheme (row SH-F32), or
// says why it is not stated.
func (l *fastapiShapes) scheme(s securityScheme) (*routeshape.Scheme, string) {
	name, ok := s.SchemeName.(string)
	if !ok || name == "unknown" {
		return nil, s.Class + ": scheme name not read"
	}
	file, binding, _ := strings.Cut(s.Instance, "#")
	p := l.files[file].Python
	if p == nil || len(p.Scopes) == 0 {
		return nil, name + ": instance not read"
	}
	b, found := p.Scopes[0].Bindings[binding]
	if !found || b.Value.Kind != "call" {
		return nil, name + ": instance not read"
	}
	call := b.Value
	if call.Object != nil {
		if cb := l.modules.Expression(file, 0, *call.Object); !cb.External {
			// A repository subclass: its own code decides what is read
			// (SH-F13's refusal); the scheme is not stated.
			tagShape("SH-F32/subclass")
			return nil, name + ": a repository subclass of " + s.Class + " is not read"
		}
	}
	text := func(kw string, position int) (*string, string) {
		e, present := argumentOf(call, kw, position)
		if position < 0 {
			e, present = keywordOf(call, kw)
		}
		if !present {
			return nil, kw + " not given"
		}
		v, resolution, valid := l.modules.Prefix(file, 0, e)
		if !valid {
			return nil, kw + " is not a literal"
		}
		if resolution == "settings_default" {
			return nil, kw + " is a settings default (overridable from the environment)"
		}
		if v == "" {
			return nil, kw + " is empty"
		}
		return &v, ""
	}
	sc := &routeshape.Scheme{Name: name, Provenance: routeshape.Provenance{Kind: routeshape.Fact, Rule: "SH-F32", File: file, Line: lineOf(p, binding), Chain: []routeshape.Step{}, Confidence: 1}}
	switch s.Class {
	case "OAuth2PasswordBearer":
		// The scheme and its type are FastAPI's; a tokenUrl computed (an
		// f-string, a settings default) is a detail not read: null
		// (SM-19), never a guess.
		v, _ := text("tokenUrl", 0)
		if v == nil {
			if _, present := argumentOf(call, "tokenUrl", 0); !present {
				return nil, name + ": tokenUrl not given"
			}
			tagShape("SH-F32/token_url null")
		}
		sc.Type, sc.TokenURL = routeshape.SchemeOAuth2, v
	case "APIKeyHeader", "APIKeyQuery", "APIKeyCookie":
		v, why := text("name", -1)
		if v == nil {
			return nil, name + ": key name not read (" + why + ")"
		}
		in := strings.ToLower(strings.TrimPrefix(s.Class, "APIKey"))
		sc.Type, sc.ParamName, sc.In = routeshape.SchemeAPIKey, v, &in
	case "HTTPBearer":
		sc.Type, sc.Scheme = routeshape.SchemeHTTP, strPtr("bearer")
		if _, present := keywordOf(call, "bearerFormat"); present {
			v, why := text("bearerFormat", -1)
			if v == nil {
				return nil, name + ": bearerFormat not read (" + why + ")"
			}
			sc.BearerFormat = v
		}
	case "HTTPBasic":
		sc.Type, sc.Scheme = routeshape.SchemeHTTP, strPtr("basic")
	case "HTTPDigest":
		sc.Type, sc.Scheme = routeshape.SchemeHTTP, strPtr("digest")
	case "OpenIdConnect":
		v, why := text("openIdConnectUrl", -1)
		if v == nil {
			return nil, name + ": openIdConnectUrl not read (" + why + ")"
		}
		sc.Type, sc.OpenIDConnectURL = routeshape.SchemeOpenIDConnect, v
	default:
		return nil, name + ": " + s.Class + " is not in the scheme model"
	}
	for _, kw := range call.Keywords {
		if kw.Name == "" {
			return nil, name + ": keyword unpacking"
		}
	}
	tagShape("SH-F32/" + s.Class)
	return sc, ""
}

func keywordOf(e facts.RouteExpr, name string) (facts.RouteExpr, bool) {
	for _, kw := range e.Keywords {
		if kw.Name == name {
			return kw.Value, true
		}
	}
	return facts.RouteExpr{}, false
}

// lineOf is the line of a module-level binding (from its value's span,
// else the first line).
func lineOf(p *facts.PythonFacts, name string) int {
	b := p.Scopes[0].Bindings[name]
	if b.Value.Span != nil && b.Value.Span.StartLine > 0 {
		return b.Value.Span.StartLine
	}
	return 1
}

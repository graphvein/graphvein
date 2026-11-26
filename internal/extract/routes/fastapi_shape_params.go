package routes

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/python"
)

// This file classifies parameters as FastAPI does (fastapi 0.161.1
// dependencies/utils.py analyze_param, rows SH-F1..SH-F21) and reads the
// request object, returned values and raised exceptions of followed
// functions (rows SH-F15..SH-F17, SH-F29, SH-F31).

// markerOf names the FastAPI marker a call is: Depends, Security, Query,
// Path, Body, Header, Cookie, Form, File; "Field" for pydantic's Field;
// "" for a value FastAPI ignores; "?" for one not read. metadata is true
// for an Annotated item, where only allowedMetadata is ignored (a
// BeforeValidator changes what is accepted, row SH-F22's refusal).
func (r *routeBuild) markerOf(file string, scope int, e facts.RouteExpr, metadata bool) string {
	switch e.Kind {
	case "call":
	case "literal", "string", "integer", "float", "true", "false", "none", "ellipsis":
		return ""
	default:
		return "?"
	}
	if e.Object == nil {
		return "?"
	}
	b := r.l.modules.Expression(file, scope, *e.Object)
	if !b.External {
		return "?"
	}
	name := fullName(b)
	at := strings.LastIndex(name, ".")
	if at < 0 {
		return "?"
	}
	module, short := name[:at], name[at+1:]
	switch module {
	case "fastapi", "fastapi.param_functions", "fastapi.params":
		switch short {
		case "Depends", "Security", "Query", "Path", "Body", "Header", "Cookie", "Form", "File":
			return short
		}
		return "?"
	case "pydantic", "pydantic.fields", "sqlmodel":
		if short == "Field" {
			return "Field"
		}
	}
	if metadata && !allowedMetadata(name) {
		tagShape("SH-F22/metadata")
		return "?"
	}
	return ""
}

// constraintKeywords are marker keywords that reject some values (a 422
// can be reached, row SH-F30).
var constraintKeywords = map[string]bool{"gt": true, "ge": true, "lt": true, "le": true, "min_length": true, "max_length": true,
	"pattern": true, "regex": true, "multiple_of": true, "max_digits": true, "decimal_places": true}

// markerDefault reads a marker call's default: required (no default or
// Ellipsis), optional, or ok=false when it is not read.
func markerDefault(call facts.RouteExpr, markerName string) (routeshape.Presence, bool) {
	if markerName == "Depends" || markerName == "Security" {
		return routeshape.Required, true
	}
	for _, k := range call.Keywords {
		switch k.Name {
		case "":
			return "", false
		case "default":
			if k.Value.Kind == "ellipsis" {
				return routeshape.Required, true
			}
			return routeshape.Optional, true
		case "default_factory":
			return routeshape.Optional, true
		}
	}
	if len(call.Args) > 0 {
		if call.Args[0].Kind == "ellipsis" {
			return routeshape.Required, true
		}
		return routeshape.Optional, true
	}
	return routeshape.Required, true
}

// parameter classifies one parameter of a dependency-tree function and
// returns the dependency call it declares, if any.
func (r *routeBuild) parameter(sf *shapeFunc, p facts.PythonShapeParam, frame routeshape.Frame) []routeshape.Call {
	file, scope := sf.file, r.enclosing(sf)
	problem := func(text string) []routeshape.Call {
		tagShape("D")
		for _, s := range paramSections {
			r.reason(s, fmt.Sprintf("parameter %s (%s:%d): %s", p.Name, sf.file, p.Line, text))
		}
		return nil
	}
	if p.Kind == "args" || p.Kind == "kwargs" {
		return problem("*args and **kwargs are not read")
	}
	var typ *metadataItem
	var items []metadataItem
	alias := false
	if p.Annotation != nil {
		if a := *p.Annotation; a.Kind == "identifier" || a.Kind == "attribute" {
			b := r.l.modules.Expression(file, scope, a)
			alias = !b.External && b.File != "" && b.Scope == 0 && b.Syntax.Value.Kind == "subscript"
		}
		typ, items = r.l.sec.flatten(file, scope, *p.Annotation, map[string]bool{})
	}
	marker, markerName := (*metadataItem)(nil), ""
	constrained := false
	for i := range items {
		if items[i].expr.Kind == "call" && items[i].expr.Object != nil {
			if b := r.l.modules.Expression(items[i].file, items[i].scope, *items[i].expr.Object); b.External && constraintMetadata(fullName(b), items[i].expr) {
				constrained = true
			}
		}
		switch m := r.markerOf(items[i].file, items[i].scope, items[i].expr, true); m {
		case "":
		case "?":
			return problem("an Annotated item is not read: " + describe(items[i].expr))
		case "Field":
			return problem("pydantic Field() in a parameter annotation is not read")
		default:
			marker, markerName = &items[i], m
		}
	}
	if alias && len(items) > 0 {
		tagShape("SH-F7")
	}
	inAnnotated := marker != nil
	if p.Default != nil && p.Default.Kind == "call" {
		switch m := r.markerOf(file, scope, *p.Default, false); m {
		case "":
		case "?":
			if marker != nil {
				break
			}
			return problem("the default is not read: " + describe(*p.Default))
		case "Field":
			return problem("pydantic Field() as a parameter default is not read")
		default:
			if marker != nil {
				return problem("both an Annotated marker and a marker default")
			}
			marker, markerName = &metadataItem{file, scope, *p.Default}, m
		}
	}
	site := routeshape.Step{File: sf.file, Line: p.Line, Function: p.Name}
	if markerName == "Depends" || markerName == "Security" {
		r.checkScopes(marker.expr)
		target, ok := argumentOf(marker.expr, "dependency", 0)
		tfile, tscope := marker.file, marker.scope
		if !ok || target.Kind == "none" {
			if typ == nil {
				return problem("Depends() without a callable or an annotation")
			}
			target, tfile, tscope = typ.expr, typ.file, typ.scope
		}
		site.Function = compactCall(marker.expr)
		if c, ok := r.dependencyTarget(tfile, tscope, target, site, frame); ok {
			return []routeshape.Call{c}
		}
		return nil
	}
	var info typeInfo
	annotated := typ != nil
	if annotated {
		info = r.l.types.resolve(typ.file, typ.scope, typ.expr, 0, &r.refs)
		if info.alias {
			tagShape("SH-F7")
		}
	}
	if info.cat == catSpecial {
		switch info.special {
		case "Request", "HTTPConnection", "WebSocket":
			return r.readsIn(sf, frame, followValue{kind: "req", param: p.Name, dep: frame.Depth > 0}, true)
		case "Response":
			// The shared response object: setting its status (or any use
			// not read) overrides the declared success response; cookies
			// and headers set on it do not change the status (SH-F24).
			for _, s := range sf.fn.Sites {
				if s.Kind == "read" && s.Name == p.Name && (len(s.Attrs) == 0 && (s.Method == "set_cookie" || s.Method == "delete_cookie") || len(s.Attrs) > 0 && s.Attrs[0] == "headers") {
					tagShape("SH-F24/cookie")
					continue
				}
				if (s.Kind == "read" || s.Kind == "use") && s.Name == p.Name {
					tagShape("SH-F24/override")
					r.override = append(r.override, fmt.Sprintf("the Response parameter %s is changed (%s:%d)", p.Name, sf.file, s.Line))
					break
				}
				for _, a := range s.Args {
					if a.Param == p.Name && s.Kind != "read" {
						r.override = append(r.override, fmt.Sprintf("the Response parameter %s is passed on (%s:%d)", p.Name, sf.file, s.Line))
					}
				}
			}
		}
		return nil
	}
	// The presence and wire name.
	presence := routeshape.Required
	if p.HasDefault {
		presence = routeshape.Optional
	}
	if marker != nil && !inAnnotated {
		pr, ok := markerDefault(marker.expr, markerName)
		if !ok {
			return problem("the marker's default is not read")
		}
		presence = pr
	}
	name := p.Name
	embed, mediaType := false, ""
	if marker != nil {
		for _, k := range marker.expr.Keywords {
			if constraintKeywords[k.Name] {
				constrained = true
			}
			switch k.Name {
			case "alias":
				if k.Value.Kind != "literal" {
					return problem("alias is not a literal")
				}
				name = k.Value.Name
			case "validation_alias", "serialization_alias", "include_in_schema", "alias_priority":
				return problem(k.Name + "= is not read")
			case "convert_underscores":
				if k.Value.Kind != "false" && k.Value.Kind != "true" {
					return problem("convert_underscores is not a literal")
				}
			case "embed":
				if k.Value.Kind != "false" && k.Value.Kind != "true" {
					return problem("embed is not a literal")
				}
				embed = k.Value.Kind == "true"
			case "media_type":
				if k.Value.Kind != "literal" {
					return problem("media_type is not a literal")
				}
				mediaType = k.Value.Name
			}
		}
		if markerName == "Header" && name == p.Name {
			convert := true
			for _, k := range marker.expr.Keywords {
				if k.Name == "convert_underscores" {
					convert = k.Value.Kind == "true"
				}
			}
			if convert {
				name = strings.ReplaceAll(name, "_", "-")
			}
		}
	}
	schema := info.schema
	if !annotated {
		schema = routeshape.Schema{Type: routeshape.TypeUnknown, Reason: "no annotation: any value"}
	}
	itemProv := func(rule string) routeshape.Provenance {
		return prov(routeshape.Fact, rule, sf.file, p.Line, frame)
	}
	// loose: every value a client sends for this item validates.
	loose := (!annotated || info.loose) && !constrained
	addParam := func(section, rule string, presence routeshape.Presence) []routeshape.Call {
		tagShape(rule)
		s := schema
		r.addParam(section, routeshape.Param{Name: name, Access: routeshape.Declared, Presence: presence, Schema: &s, Provenance: itemProv(rule)})
		r.declared = true
		// A path parameter is always present once the route matched.
		if !loose || presence == routeshape.Required && section != routeshape.SectionPath {
			r.canFail = true
		}
		return nil
	}
	addBody := func(rule string, form, file bool) []routeshape.Call {
		tagShape(rule)
		bp := bodyParam{name: name, presence: presence, schema: schema, marker: markerName, embed: embed, mediaType: mediaType, file: file, prov: itemProv(rule), row: rule}
		if form && bp.marker == "" {
			bp.marker = "File"
		}
		r.body = append(r.body, bp)
		r.declared = true
		// A JSON body can always be sent malformed; a form field fails
		// when required or typed.
		if !form || !loose || presence == routeshape.Required {
			r.canFail = true
		}
		return nil
	}
	switch markerName {
	case "Path":
		if !r.pathNames[name] {
			return problem("Path() names no template parameter")
		}
		tagShape("SH-F1/Path()")
		return addParam(routeshape.SectionPath, "SH-F1", routeshape.Required)
	case "Query", "Header", "Cookie":
		if info.cat == catModel {
			return problem(markerName + "() with a model type is not read")
		}
		tagShape("SH-F5/" + markerName + "()")
		section := map[string]string{"Query": routeshape.SectionQuery, "Header": routeshape.SectionHeader, "Cookie": routeshape.SectionCookie}[markerName]
		return addParam(section, "SH-F5", presence)
	case "Body":
		if embed {
			tagShape("SH-F19/embed")
		}
		return addBody("SH-F18", false, false)
	case "Form", "File":
		if info.cat == catModel {
			return r.formModel(sf, p, info, markerName, frame, problem)
		}
		return addBody("SH-F20", true, markerName == "File")
	}
	// No marker: FastAPI's order is path name, scalar, then body.
	if r.pathNames[p.Name] {
		if annotated && info.cat != catScalar {
			return problem("a path parameter that is not a scalar")
		}
		return addParam(routeshape.SectionPath, "SH-F1", routeshape.Required)
	}
	if !annotated {
		return addParam(routeshape.SectionQuery, "SH-F6", presence)
	}
	switch info.cat {
	case catScalar:
		return addParam(routeshape.SectionQuery, "SH-F4", presence)
	case catFile:
		return addBody("SH-F20", true, true)
	case catSequence, catMapping:
		tagShape("SH-F21")
		return addBody("SH-F21", false, false)
	case catModel:
		return addBody("SH-F18", false, false)
	}
	return problem("the annotation is not read: " + info.schema.Reason)
}

// formModelVersion is the FastAPI release that reads a Form() parameter
// typed with a model as the model's fields (0.113.0).
var formModelVersion = python.Version{0, 113, 0}

// formModel reads a Form() parameter typed with a model (row SH-F20):
// FastAPI (>= 0.113.0, proven from the manifest) reads the model's fields
// as the form fields, so the body schema is the model's.
func (r *routeBuild) formModel(sf *shapeFunc, p facts.PythonShapeParam, info typeInfo, markerName string, frame routeshape.Frame, problem func(string) []routeshape.Call) []routeshape.Call {
	if markerName != "Form" {
		return problem("File() with a model type is not read")
	}
	v, manifest, ok := r.l.modules.FastAPIMinimum(r.in.file)
	if !ok || !v.AtLeast(formModelVersion) {
		why := "no pyproject.toml requires fastapi with a lower bound"
		if manifest != "" {
			why = manifest + " allows FastAPI " + v.String()
			if !ok {
				why = manifest + ": the fastapi requirement is not read"
			}
		}
		return problem("Form() with a model is read as the model's fields from FastAPI 0.113.0 (" + why + ")")
	}
	if e := r.l.types.models[r.l.types.names[info.schema.Ref]]; e == nil || e.file {
		return problem("a form model with a file field is not read")
	}
	tagShape("SH-F20/form model")
	r.body = append(r.body, bodyParam{name: p.Name, presence: routeshape.Required, schema: info.schema, marker: "Form", formModel: true,
		prov: prov(routeshape.Fact, "SH-F20", sf.file, p.Line, frame), row: "SH-F20"})
	r.declared, r.canFail = true, true
	return nil
}

// addParam adds a parameter once per (name, kind); a repeated declaration
// with another schema or presence keeps the name without a schema.
func (r *routeBuild) addParam(section string, p routeshape.Param) {
	key := func(n string) string {
		if section == routeshape.SectionHeader {
			return strings.ToLower(n)
		}
		return n
	}
	for i, old := range r.params[section] {
		if key(old.Name) == key(p.Name) && old.Provenance.Kind == p.Provenance.Kind {
			if p.Provenance.Kind == routeshape.Fact && (old.Presence != p.Presence || !sameSchema(old.Schema, p.Schema)) {
				r.params[section][i].Schema = nil
				if p.Presence == routeshape.Required {
					r.params[section][i].Presence = routeshape.Required
				}
				r.reason(section, "parameter "+p.Name+" is declared twice with different types")
			}
			return
		}
	}
	r.params[section] = append(r.params[section], p)
}

func sameSchema(a, b *routeshape.Schema) bool {
	x, _ := jsonText(a)
	y, _ := jsonText(b)
	return x == y
}

// sectionOf maps a request attribute to its section.
var requestSections = map[string]string{"headers": routeshape.SectionHeader, "cookies": routeshape.SectionCookie, "query_params": routeshape.SectionQuery, "path_params": routeshape.SectionPath}

// rawBody are the request methods that read the body (row SH-F16).
var rawBody = map[string]bool{"json": true, "body": true, "form": true, "stream": true}

// reads reads a function reached with a request value (row SH-F17): its
// reads, the calls it passes the value to, and its raises.
func (r *routeBuild) reads(sf *shapeFunc, frame routeshape.Frame, v followValue) []routeshape.Call {
	r.raises(sf, frame, v)
	return r.readsIn(sf, frame, v, false)
}

// escape marks every section the value can reach as partial.
func (r *routeBuild) escape(v followValue, text string) {
	if section, ok := requestSections[v.kind]; ok {
		r.reason(section, text)
		return
	}
	for _, s := range paramSections {
		r.reason(s, text)
	}
}

// readsIn records the reads of one request value in one function and
// returns the calls it is passed on through.
func (r *routeBuild) readsIn(sf *shapeFunc, frame routeshape.Frame, v followValue, declared bool) []routeshape.Call {
	if declared {
		tagShape("SH-F14/Request")
	}
	access := routeshape.ReadByHandler
	if v.dep {
		access = routeshape.ReadByDependency
	}
	read := func(section, key string, line int) {
		tagShape("SH-F15/" + section)
		r.addParam(section, routeshape.Param{Name: key, Access: access, Presence: routeshape.PresenceUnknown, Provenance: prov(routeshape.ReadInHandler, "SH-F15", sf.file, line, frame)})
		r.reason(section, "names read in code; their types and whether they must be sent are not read")
	}
	var calls []routeshape.Call
	for _, s := range sf.fn.Sites {
		switch s.Kind {
		case "read":
			if s.Name != v.param {
				continue
			}
			section, mapping := requestSections[v.kind]
			attrs := s.Attrs
			if !mapping {
				if len(attrs) == 0 {
					switch {
					case rawBody[s.Method]:
						tagShape("SH-F16")
						ct := (*string)(nil)
						c := routeshape.Content{ContentType: ct, Provenance: prov(routeshape.ReadInHandler, "SH-F16", sf.file, s.Line, frame)}
						if len(r.rawBody) == 0 {
							r.rawBody = append(r.rawBody, c)
						}
						r.reason(routeshape.SectionBody, fmt.Sprintf("the raw request body is read (%s:%d)", sf.file, s.Line))
					case harmlessAttribute[s.Method] || s.Method == "is_disconnected":
					default:
						r.escape(v, fmt.Sprintf("the request is read in a way not modelled (%s:%d)", sf.file, s.Line))
					}
					continue
				}
				if harmlessAttribute[attrs[0]] {
					continue
				}
				sec, ok := requestSections[attrs[0]]
				if !ok {
					r.escape(v, fmt.Sprintf("the request is read in a way not modelled (%s:%d)", sf.file, s.Line))
					continue
				}
				section, attrs = sec, attrs[1:]
			}
			if len(attrs) == 0 && (s.Method == "" || s.Method == "get" || s.Method == "getlist") && s.Key != nil {
				read(section, *s.Key, s.Line)
				continue
			}
			r.reason(section, fmt.Sprintf("a name read is not a literal (%s:%d)", sf.file, s.Line))
		case "use":
			if s.Name == v.param {
				tagShape("SH-F17/use")
				r.escape(v, fmt.Sprintf("the request value is used where its reads are not followed (%s:%d)", sf.file, s.Line))
			}
		case "call", "assign", "return":
			for _, a := range s.Args {
				if a.Param != v.param {
					continue
				}
				next := v
				next.tainted = v.tainted || s.Try
				if a.Attr != "" {
					if _, ok := requestSections[a.Attr]; ok && v.kind == "req" {
						next.kind = a.Attr
					} else if harmlessAttribute[a.Attr] {
						continue
					} else {
						r.escape(v, fmt.Sprintf("an attribute of the request value is passed on (%s:%d)", sf.file, s.Line))
						continue
					}
				}
				tagShape("SH-F17")
				calls = append(calls, r.mark(r.valueCall(sf, s, a, next), next))
			}
		}
	}
	return calls
}

// valueCall resolves a call site that passes a followed value as argument
// a: a proven repository function (with the callee's parameter name) or
// a stop.
func (r *routeBuild) valueCall(sf *shapeFunc, s facts.PythonShapeSite, a facts.PythonShapeArg, v followValue) routeshape.Call {
	site := routeshape.Step{File: sf.file, Line: s.Line, Function: compactCall(*s.Expr.Object)}
	stop := func(st routeshape.Stop, detail string) routeshape.Call {
		return routeshape.Call{Site: site, Stop: st, Detail: detail}
	}
	b, call, ok := r.callee(sf, s, site)
	if !ok {
		return call
	}
	if b.Syntax.Value.Kind != "function_definition" {
		return stop(routeshape.StopUnresolved, "the value is passed to "+b.Name+", which is not a function")
	}
	target := r.l.byScope[fmt.Sprintf("%s:%d", b.File, b.Syntax.ClassScope)]
	if target == nil {
		c, _ := r.proven(b, b.Syntax.ClassScope, site, followValue{kind: v.kind, param: "?", dep: v.dep, tainted: v.tainted})
		return c
	}
	if s.Expr.Invalid {
		return stop(routeshape.StopUnresolved, "argument unpacking: the parameter is not bound")
	}
	param := ""
	splat := false
	positional := 0
	for _, p := range target.fn.Params {
		switch p.Kind {
		case "args", "kwargs":
			splat = true
		case "keyword":
			if a.Index < 0 && p.Name == a.Keyword {
				param = p.Name
			}
		default:
			if a.Index >= 0 && positional == a.Index {
				param = p.Name
			}
			if a.Index < 0 && p.Kind != "positional" && p.Name == a.Keyword {
				param = p.Name
			}
			positional++
		}
	}
	if param == "" {
		if splat {
			return stop(routeshape.StopTransformed, "the value is passed to *args or **kwargs")
		}
		return stop(routeshape.StopUnresolved, "the argument binds no parameter")
	}
	next := v
	next.param = param
	return routeshape.Call{Site: site, Target: target.step, Value: next.String()}
}

// callee resolves a call site's callee to a unique repository binding; it
// returns the stop call otherwise.
func (r *routeBuild) callee(sf *shapeFunc, s facts.PythonShapeSite, site routeshape.Step) (python.Binding, routeshape.Call, bool) {
	stop := func(st routeshape.Stop, detail string) (python.Binding, routeshape.Call, bool) {
		return python.Binding{}, routeshape.Call{Site: site, Stop: st, Detail: detail}, false
	}
	if s.Expr == nil || s.Expr.Object == nil {
		return stop(routeshape.StopUnresolved, "the callee is not read")
	}
	b := r.l.modules.Expression(sf.file, sf.scope, *s.Expr.Object)
	switch {
	case b.External:
		return stop(routeshape.StopExternal, fullName(b))
	case b.File == "" || b.Module != nil:
		return stop(routeshape.StopUnresolved, describe(*s.Expr.Object))
	case b.Syntax.Count != 1:
		return stop(routeshape.StopAmbiguous, describe(*s.Expr.Object)+" is bound more than once")
	}
	if b.Syntax.Value.Kind == "function_definition" {
		p := r.l.files[b.File].Python
		if p != nil && b.Syntax.ClassScope < len(p.Scopes) {
			parent := p.Scopes[b.Syntax.ClassScope].Parent
			if parent >= 0 && parent < len(p.Scopes) && p.Scopes[parent].Class {
				return stop(routeshape.StopUnresolved, "a method call is not followed")
			}
		}
	}
	return b, routeshape.Call{}, true
}

// handlerReturns follows the handler's returned values when no response
// model is declared (row SH-F31).
func (r *routeBuild) handlerReturns(sf *shapeFunc, frame routeshape.Frame) []routeshape.Call {
	if _, ok := keyword(r.in.syntax.Expression, "response_model"); ok || sf.fn.Returns != nil || sf.fn.Generator {
		return nil
	}
	return r.returnSites(sf, frame, followValue{kind: "ret"})
}

// returns reads a function reached with a returned value.
func (r *routeBuild) returns(sf *shapeFunc, frame routeshape.Frame, v followValue) []routeshape.Call {
	r.raises(sf, frame, v)
	if sf.fn.Returns != nil {
		info := r.l.types.resolve(sf.file, r.enclosing(sf), *sf.fn.Returns, 0, &r.refs)
		switch info.cat {
		case catSpecial, catNone:
			r.reason(routeshape.SectionResponses, fmt.Sprintf("%s returns %s: its body is not read", sf.step.Function, describe(*sf.fn.Returns)))
			return nil
		}
		tagShape("SH-F31")
		r.returned = append(r.returned, routeshape.Response{Schema: &info.schema, Provenance: prov(routeshape.Returned, "SH-F31", sf.file, sf.fn.Line, frame)})
		return nil
	}
	return r.returnSites(sf, frame, v)
}

// returnSites reads the return statements of a function.
func (r *routeBuild) returnSites(sf *shapeFunc, frame routeshape.Frame, v followValue) []routeshape.Call {
	var calls []routeshape.Call
	assigns := map[string][]facts.PythonShapeSite{}
	for _, s := range sf.fn.Sites {
		if s.Kind == "assign" {
			assigns[s.Name] = append(assigns[s.Name], s)
		}
	}
	p := r.l.files[sf.file].Python
	for _, s := range sf.fn.Sites {
		if s.Kind != "return" {
			continue
		}
		if s.Name != "" {
			if len(assigns[s.Name]) != 1 || p.Scopes[sf.scope].Bindings[s.Name].Count != 1 {
				r.reason(routeshape.SectionResponses, fmt.Sprintf("the returned value %s is not read (%s:%d)", s.Name, sf.file, s.Line))
				continue
			}
			s = assigns[s.Name][0]
		}
		if s.Expr == nil {
			r.reason(routeshape.SectionResponses, fmt.Sprintf("a returned value is not read (%s:%d)", sf.file, s.Line))
			continue
		}
		site := routeshape.Step{File: sf.file, Line: s.Line, Function: compactCall(*s.Expr.Object)}
		b, stop, ok := r.callee(sf, s, site)
		if !ok {
			calls = append(calls, r.mark(stop, v))
			continue
		}
		switch b.Syntax.Value.Kind {
		case "class_definition":
			var refs []string
			info := r.l.types.class(b, 0, &refs)
			r.refs = append(r.refs, refs...)
			tagShape("SH-F31/constructor")
			r.returned = append(r.returned, routeshape.Response{Schema: &info.schema, Provenance: prov(routeshape.Returned, "SH-F31", sf.file, s.Line, frame)})
		case "function_definition":
			next := v
			next.tainted = v.tainted || s.Try
			c, _ := r.proven(b, b.Syntax.ClassScope, site, next)
			calls = append(calls, r.mark(c, next))
		default:
			calls = append(calls, r.mark(routeshape.Call{Site: site, Stop: routeshape.StopUnresolved, Detail: b.Name + " is not a function"}, v))
		}
	}
	return calls
}

// httpExceptions are the classes whose raise sets the response status
// (row SH-F29).
var httpExceptions = map[string]bool{"fastapi.HTTPException": true, "fastapi.exceptions.HTTPException": true, "starlette.exceptions.HTTPException": true}

// statusCode reads a status code: an integer literal or a fastapi /
// starlette status constant.
func (r *routeBuild) statusCode(file string, scope int, e facts.RouteExpr) (string, bool) {
	switch e.Kind {
	case "integer":
		n, err := strconv.Atoi(e.Name)
		if err != nil || n < 100 || n > 599 {
			return "", false
		}
		return strconv.Itoa(n), true
	case "identifier", "attribute":
		b := r.l.modules.Expression(file, scope, e)
		if !b.External {
			return "", false
		}
		name := fullName(b)
		at := strings.LastIndex(name, ".")
		module, short := name[:at+1], name[at+1:]
		if module != "fastapi.status." && module != "starlette.status." || !strings.HasPrefix(short, "HTTP_") || len(short) < 8 {
			return "", false
		}
		n, err := strconv.Atoi(short[5:8])
		if err != nil || n < 100 || n > 599 {
			return "", false
		}
		return strconv.Itoa(n), true
	}
	return "", false
}

// raises records the HTTPException raises of a followed function.
func (r *routeBuild) raises(sf *shapeFunc, frame routeshape.Frame, v followValue) {
	for _, s := range sf.fn.Sites {
		if s.Kind != "raise" || s.Expr == nil || s.Expr.Object == nil {
			continue
		}
		b := r.l.modules.Expression(sf.file, sf.scope, *s.Expr.Object)
		if !b.External || !httpExceptions[fullName(b)] {
			if !b.External && b.Syntax.Value.Kind == "class_definition" {
				r.reason(routeshape.SectionResponses, fmt.Sprintf("the status of a raised %s is not read (%s:%d)", b.Name, sf.file, s.Line))
			}
			continue
		}
		if s.Try || v.tainted {
			r.reason(routeshape.SectionResponses, fmt.Sprintf("an HTTPException raised inside try may be caught (%s:%d)", sf.file, s.Line))
			continue
		}
		if s.AfterYield {
			tagShape("SH-F29/after yield")
			r.reason(routeshape.SectionResponses, fmt.Sprintf("an HTTPException raised after yield in %s runs after the response is sent (%s:%d)", sf.step.Function, sf.file, s.Line))
			continue
		}
		code, ok := "", false
		if e, found := argumentOf(*s.Expr, "status_code", 0); found {
			code, ok = r.statusCode(sf.file, sf.scope, e)
		}
		if !ok {
			r.reason(routeshape.SectionResponses, fmt.Sprintf("the status of an HTTPException is not read (%s:%d)", sf.file, s.Line))
			continue
		}
		tagShape("SH-F29")
		kind := routeshape.Fact
		if handlers := append(append([]string{}, r.l.handlerHTTP...), r.l.handlerStatus[code]...); len(handlers) > 0 {
			tagShape("SH-F37")
			kind = routeshape.Intended
			for _, h := range handlers {
				r.reason(routeshape.SectionResponses, h+" can rewrite a raised HTTPException")
			}
		}
		r.addResponse(routeshape.Response{Status: code, Provenance: prov(kind, "SH-F29", sf.file, s.Line, frame)})
	}
}

// addResponse adds a response once per (status, content type, kind).
func (r *routeBuild) addResponse(resp routeshape.Response) {
	ct := "\x00"
	if resp.ContentType != nil {
		ct = *resp.ContentType
	}
	key := resp.Status + "\x00" + ct + "\x00" + string(resp.Provenance.Kind)
	if r.statusSeen[key] {
		return
	}
	r.statusSeen[key] = true
	r.responses = append(r.responses, resp)
}

// statusInt reads a status code string as a number (0 for ranges).
func statusInt(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// harmlessAttribute lists request attributes that read no parameter,
// header, cookie or body (Starlette's Request). url (the query string and
// path) and state (filled from the request by middleware and
// dependencies) are not harmless (SH-F17).
var harmlessAttribute = map[string]bool{"base_url": true, "client": true, "app": true, "method": true, "url_for": true}

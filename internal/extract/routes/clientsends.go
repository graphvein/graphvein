package routes

import (
	"fmt"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/shapediff"
)

// What a client call sends (T029 W4, rows SD-1..SD-5 of
// docs/contract/claims/shape-differences.md). LinkClientSends records,
// on the client_call nodes the clients linker made, the keys a call
// provably sends: the keys of an object literal written at the call site
// (SD-2, SD-3), or the declared type of a proven hey-api operation (SD-4,
// SD-5). It reads only cached syntax at link time. Every other form (D)
// records nothing, so the server compares nothing for that call.

// LinkClientSends sets the shapediff.AttributeKey attribute on the
// client_call nodes whose sent keys are proven. A call that is not read
// is left unchanged; a value the attribute check refuses is not stored.
func LinkClientSends(nodes []contract.Node, files map[string]facts.Facts, modules *typescript.ModuleResolver) []contract.Diagnostic {
	r := &resolver{files: files, objects: map[string]string{}, objectFiles: map[string]string{}, modules: modules}
	sr := &schemaReader{r: r, models: newModelSet()}
	sites := map[string]facts.ClientCall{}
	for file, f := range files {
		if f.Routes == nil {
			continue
		}
		for _, c := range f.Routes.Clients {
			sites[sendSite(file, c.Span)] = c
		}
	}
	var diagnostics []contract.Diagnostic
	for i := range nodes {
		n := &nodes[i]
		if n.Kind != "client_call" || n.Span == nil || n.Attributes == nil {
			continue
		}
		// A wrapper's call sites pass arguments the wrapper rewrites, and
		// a wrapper's inner request has no arguments of its own: neither is
		// read (D).
		if w, ok := n.Attributes["wrapper"]; !ok || w != nil || n.Attributes["status"] == "wrapper" {
			continue
		}
		call, ok := sites[sendSite(n.File, *n.Span)]
		if !ok {
			continue
		}
		var s *shapediff.Sends
		switch n.Attributes["library"] {
		case "fetch":
			s = fetchSends(r, n, call)
		case "hey-api":
			s = heySends(sr, n.File, call)
		}
		if s == nil {
			continue
		}
		value, err := shapediff.ToAttribute(s)
		if err != nil {
			diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: n.File, Severity: "warning", Code: "client_sends_refused", Message: fmt.Sprintf("%s: %v", n.QualifiedName, err)})
			continue
		}
		n.Attributes[shapediff.AttributeKey] = value
	}
	return diagnostics
}

func sendSite(file string, s contract.Span) string {
	return fmt.Sprintf("%s:%d:%d:%d:%d", file, s.StartLine, s.StartColumn, s.EndLine, s.EndColumn)
}

// unboundGlobal reports a name no binding of file shadows at scope (a
// global such as JSON or URLSearchParams).
func unboundGlobal(r *resolver, file string, scope int, name string) bool {
	_, _, bound := r.binding(file, scope, name)
	return !bound
}

// fetchSends reads a fetch call (rows SD-2, SD-3).
func fetchSends(r *resolver, n *contract.Node, call facts.ClientCall) *shapediff.Sends {
	args := call.Expression.Args
	out := &shapediff.Sends{}
	line := call.Span.StartLine
	if len(args) >= 2 {
		method, _ := n.Attributes["method"].(string)
		if method != "" && method != "GET" && method != "HEAD" {
			if obj, ok := fetchJSONBody(r, n.File, call.Scope, args[1]); ok {
				part := literalPart(n.File, line, obj, true, 0)
				part.Rule, part.ContentType = "SD-2", shapediff.JSON
				out.Body = part
			}
		}
	}
	if len(args) >= 1 {
		if obj, ok := searchParamsQuery(r, n.File, call.Scope, args[0]); ok {
			part := literalPart(n.File, line, obj, false, 0)
			part.Rule = "SD-3"
			out.Query = part
		}
	}
	if out.Body == nil && out.Query == nil {
		return nil
	}
	return out
}

// fetchJSONBody returns the object literal of an init object whose own
// entries (no spread, no computed key, no method) say
// `headers: {"Content-Type": "application/json"}` and
// `body: JSON.stringify({...})` with the global JSON and one argument.
// Without the header, fetch sends text/plain, which a JSON body parser
// does not read: the body is then not compared.
func fetchJSONBody(r *resolver, file string, scope int, init facts.RouteExpr) (facts.RouteExpr, bool) {
	if init.Kind != "object" || init.Invalid || init.Spread {
		return facts.RouteExpr{}, false
	}
	headers, ok := init.Properties["headers"]
	if !ok || headers.Kind != "object" || headers.Invalid || headers.Spread {
		return facts.RouteExpr{}, false
	}
	json := 0
	for _, key := range headers.Keys {
		if !strings.EqualFold(key, "content-type") {
			continue
		}
		v := headers.Properties[key]
		if v.Kind != "literal" || mediaType(v.Name) != shapediff.JSON {
			return facts.RouteExpr{}, false
		}
		json++
	}
	if json != 1 {
		return facts.RouteExpr{}, false
	}
	body, ok := init.Properties["body"]
	if !ok || body.Kind != "call_expression" || body.Object == nil || len(body.Args) != 1 {
		return facts.RouteExpr{}, false
	}
	callee := *body.Object
	if callee.Kind != "member_expression" || callee.Name != "stringify" || callee.Object == nil || callee.Object.Kind != "identifier" || callee.Object.Name != "JSON" || !unboundGlobal(r, file, scope, "JSON") {
		return facts.RouteExpr{}, false
	}
	obj := body.Args[0]
	if obj.Kind != "object" {
		return facts.RouteExpr{}, false
	}
	return obj, true
}

func mediaType(t string) string {
	t, _, _ = strings.Cut(t, ";")
	return strings.ToLower(strings.TrimSpace(t))
}

// searchParamsQuery returns the object literal of a URL template whose
// last piece is `new URLSearchParams({...})` (or its `.toString()`),
// right after the template's only `?`.
func searchParamsQuery(r *resolver, file string, scope int, url facts.RouteExpr) (facts.RouteExpr, bool) {
	if url.Kind != "template_string" || url.Invalid || len(url.Parts) < 2 {
		return facts.RouteExpr{}, false
	}
	last := url.Parts[len(url.Parts)-1]
	if last.Kind == "call_expression" && last.Object != nil && last.Object.Kind == "member_expression" && last.Object.Name == "toString" && len(last.Args) == 0 && last.Object.Object != nil {
		last = *last.Object.Object
	}
	if last.Kind != "new_expression" || last.Object == nil || last.Object.Kind != "identifier" || last.Object.Name != "URLSearchParams" || len(last.Args) != 1 || !unboundGlobal(r, file, scope, "URLSearchParams") {
		return facts.RouteExpr{}, false
	}
	marks := 0
	for _, p := range url.Parts[:len(url.Parts)-1] {
		if p.Kind == "literal" {
			marks += strings.Count(p.Name, "?") + strings.Count(p.Name, "#")
		}
	}
	before := url.Parts[len(url.Parts)-2]
	if marks != 1 || before.Kind != "literal" || !strings.HasSuffix(before.Name, "?") {
		return facts.RouteExpr{}, false
	}
	obj := last.Args[0]
	if obj.Kind != "object" {
		return facts.RouteExpr{}, false
	}
	return obj, true
}

// literalPart reads an object literal's own literal keys. json drops a
// key whose value is written undefined (JSON.stringify leaves it out);
// URLSearchParams sends it as the string "undefined". A spread, a
// computed key, a method or an accessor leaves the part incomplete: its
// own literal keys are still sent.
func literalPart(file string, line int, obj facts.RouteExpr, json bool, depth int) *shapediff.Part {
	nested := literalNested(file, line, obj, json, depth)
	return &shapediff.Part{Form: shapediff.FormLiteral, File: file, Line: line, Complete: nested.Complete, Fields: nested.Fields}
}

func literalNested(file string, line int, obj facts.RouteExpr, json bool, depth int) *shapediff.Nested {
	out := &shapediff.Nested{Complete: !obj.Spread && !obj.Invalid, Fields: []shapediff.Field{}}
	for _, key := range obj.Keys {
		v := obj.Properties[key]
		if json && (v.Kind == "undefined" || v.Kind == "identifier" && v.Name == "undefined") {
			continue
		}
		at := line
		if v.Span != nil && v.Span.StartLine > 0 {
			at = v.Span.StartLine
		}
		f := shapediff.Field{Name: key, File: file, Line: at}
		if json && v.Kind == "object" && depth < 7 {
			f.Nested = literalNested(file, at, v, json, depth+1)
		}
		out.Fields = append(out.Fields, f)
	}
	return out
}

// heySends reads a proven hey-api request inside a generated operation
// (rows SD-4, SD-5): `client.post({url, ...options})` where options is
// the operation's parameter of type `Options<TData, ...>` (the generator's
// own generic Options) and TData a repository type with `body` or
// `query`. The request object may spread only that parameter; an own
// body, headers or body serializer leaves the body unread, an own query
// or query serializer the query.
func heySends(sr *schemaReader, file string, call facts.ClientCall) *shapediff.Sends {
	d := sr.r.files[file].Routes
	syn := sr.r.files[file].Shape
	e := call.Expression
	if d == nil || syn == nil || len(e.Args) != 1 || e.Args[0].Kind != "object" {
		return nil
	}
	config := e.Args[0]
	scope := call.Scope
	for scope > 0 && scope < len(d.Scopes) && !d.Scopes[scope].Function {
		scope = d.Scopes[scope].Parent
	}
	if scope <= 0 || scope >= len(d.Scopes) {
		return nil
	}
	var fn *facts.ShapeFunction
	for i := range syn.Functions {
		if syn.Functions[i].Offset == d.Scopes[scope].Start {
			fn = &syn.Functions[i]
		}
	}
	if fn == nil {
		return nil
	}
	param := ""
	own := map[string]bool{}
	for _, entry := range config.Keywords {
		if entry.Name != "" {
			own[entry.Name] = true
			continue
		}
		v := entry.Value
		if v.Kind != "spread_element" || len(v.Parts) != 1 || v.Parts[0].Kind != "identifier" || param != "" && param != v.Parts[0].Name {
			return nil
		}
		param = v.Parts[0].Name
	}
	// Spreads mark the object Invalid; the loop above admitted only own
	// keyed entries and spreads of one identifier.
	if param == "" {
		return nil
	}
	var data string
	for _, p := range fn.Params {
		if p.Name != param || p.Type == nil {
			continue
		}
		t := *p.Type
		if t.Kind != "name" || t.Name != "Options" || len(t.Args) < 1 || t.Args[0].Kind != "name" || len(t.Args[0].Args) != 0 {
			return nil
		}
		data = t.Args[0].Name
	}
	if data == "" {
		return nil
	}
	// The generator writes a JSON operation's own headers as
	// `{'Content-Type': 'application/json', ...options.headers}`: that
	// form keeps the body JSON. Any other own headers leave it unread.
	if own["headers"] && heyJSONHeaders(config.Properties["headers"], param) {
		delete(own, "headers")
	}
	if _, _, options, ok := sr.lookupType(file, "Options"); !ok || options.Kind != "other" || !strings.HasPrefix(options.Name, "generic ") {
		return nil
	}
	dataFile, dataName, dataType, ok := sr.lookupType(file, data)
	if !ok || dataType.Kind != "object" || dataType.Open {
		return nil
	}
	dataLine := declLine(sr, dataFile, dataName)
	if dataLine < 1 {
		return nil
	}
	out := &shapediff.Sends{}
	for _, prop := range dataType.Props {
		switch {
		case prop.Name == "body" && !own["body"] && !own["headers"] && !own["bodySerializer"]:
			if part := typedPart(sr, dataFile, dataLine, dataName, prop); part != nil {
				part.Rule, part.ContentType = "SD-4", shapediff.JSON
				out.Body = part
			}
		case prop.Name == "query" && !own["query"] && !own["querySerializer"]:
			if part := typedPart(sr, dataFile, dataLine, dataName, prop); part != nil {
				part.Rule = "SD-5"
				out.Query = part
			}
		}
	}
	if out.Body == nil && out.Query == nil {
		return nil
	}
	return out
}

// heyJSONHeaders reports an own headers object literal whose entries are
// exactly one Content-Type (any case) with the literal media type
// application/json and spreads of `param.headers`.
func heyJSONHeaders(h facts.RouteExpr, param string) bool {
	if h.Kind != "object" {
		return false
	}
	json := 0
	for _, entry := range h.Keywords {
		if entry.Name == "" {
			v := entry.Value
			if v.Kind != "spread_element" || len(v.Parts) != 1 {
				return false
			}
			m := v.Parts[0]
			if m.Kind != "member_expression" || m.Name != "headers" || m.Object == nil || m.Object.Kind != "identifier" || m.Object.Name != param {
				return false
			}
			continue
		}
		v, ok := h.Properties[entry.Name]
		if !ok || !strings.EqualFold(entry.Name, "content-type") || v.Kind != "literal" || mediaType(v.Name) != shapediff.JSON {
			return false
		}
		json++
	}
	return json == 1
}

// typedPart reads a required property of an operation's data type whose
// type is an object type: a repository type, or written inline in the
// data type (then located at the data type's declaration).
func typedPart(sr *schemaReader, dataFile string, dataLine int, dataName string, prop facts.ShapeProp) *shapediff.Part {
	if prop.Optional || hasUndefined(prop.Type) {
		return nil
	}
	if prop.Type.Kind == "object" {
		if prop.Type.Open {
			return nil
		}
		return &shapediff.Part{Form: shapediff.FormTyped, Type: dataName + "." + prop.Name, File: dataFile, Line: dataLine, Fields: typedFields(sr, dataFile, dataLine, prop.Type, 0)}
	}
	obj, name, at, line, ok := typedObject(sr, dataFile, prop.Type, 0)
	if !ok {
		return nil
	}
	return &shapediff.Part{Form: shapediff.FormTyped, Type: name, File: at, Line: line, Fields: typedFields(sr, at, line, obj, 0)}
}

// declLine is the start line of a top-level interface or type alias.
func declLine(sr *schemaReader, file, name string) int {
	for _, n := range sr.r.files[file].Nodes {
		if (n.Kind == "interface" || n.Kind == "type_alias") && n.QualifiedName == name && n.Span != nil {
			return n.Span.StartLine
		}
	}
	return 0
}

// typedObject resolves a type to an object type: a name declared in the
// repository (followed through imports and aliases), with null removed
// from a union. It returns the declaration's name, file and line.
func typedObject(sr *schemaReader, file string, t facts.ShapeType, depth int) (facts.ShapeType, string, string, int, bool) {
	if depth > 4 {
		return facts.ShapeType{}, "", "", 0, false
	}
	if t.Kind == "union" {
		var rest []facts.ShapeType
		for _, a := range t.Args {
			if a.Kind != "null" {
				rest = append(rest, a)
			}
		}
		if len(rest) != 1 {
			return facts.ShapeType{}, "", "", 0, false
		}
		t = rest[0]
	}
	if t.Kind != "name" || len(t.Args) > 0 {
		return facts.ShapeType{}, "", "", 0, false
	}
	declFile, declName, decl, ok := sr.lookupType(file, t.Name)
	if !ok {
		return facts.ShapeType{}, "", "", 0, false
	}
	if decl.Kind == "name" {
		return typedObject(sr, declFile, decl, depth+1)
	}
	if decl.Kind != "object" || decl.Open {
		return facts.ShapeType{}, "", "", 0, false
	}
	line := declLine(sr, declFile, declName)
	if line < 1 {
		return facts.ShapeType{}, "", "", 0, false
	}
	return decl, declName, declFile, line, true
}

// typedFields lists an object type's required properties, with the
// object types under them; an optional property may be left out by the
// caller, so it is not claimed as sent.
func typedFields(sr *schemaReader, file string, line int, obj facts.ShapeType, depth int) []shapediff.Field {
	out := []shapediff.Field{}
	for _, p := range obj.Props {
		if p.Optional || hasUndefined(p.Type) {
			continue
		}
		f := shapediff.Field{Name: p.Name, File: file, Line: line}
		if depth < 7 && p.Type.Kind == "object" && !p.Type.Open {
			f.Nested = &shapediff.Nested{Fields: typedFields(sr, file, line, p.Type, depth+1)}
		} else if depth < 7 {
			if inner, _, at, innerLine, ok := typedObject(sr, file, p.Type, 0); ok {
				f.Nested = &shapediff.Nested{Fields: typedFields(sr, at, innerLine, inner, depth+1)}
			}
		}
		out = append(out, f)
	}
	return out
}

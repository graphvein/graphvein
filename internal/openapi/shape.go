package openapi

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/routeshape"
)

// ShapeExt is an operation's x-graphvein.shape (rows OG-70..OG-89): the
// state of every section of the route's stored shape, the items that are
// not written as standard OpenAPI fields (with why), the models they refer
// to, and how call following ended.
type ShapeExt struct {
	Version    int                   `json:"version"`
	DepthLimit int                   `json:"depth_limit"`
	Sections   map[string]SectionExt `json:"sections"`
	// NotStandard are the items kept out of parameters, requestBody,
	// responses and security: names read in code, intended, returned
	// and mapped items, and fact items a row refuses.
	NotStandard []ShapeItem        `json:"not_standard"`
	Models      []routeshape.Model `json:"models"`
	// DepthCap are the chains that stopped at the depth limit (OG-89):
	// more may be read with a higher index.shape_depth.
	DepthCap []routeshape.ChainRecord `json:"depth_cap"`
	// Stops counts the followed chains by stop reason.
	Stops map[string]int `json:"stops"`
}

// SectionExt is one section's state as stored (OG-71) and how many of its
// items are written as standard fields.
type SectionExt struct {
	State    string                 `json:"state"`
	Reason   string                 `json:"reason,omitempty"`
	Evidence *routeshape.Provenance `json:"evidence,omitempty"`
	Standard int                    `json:"standard"`
}

// ShapeItem is one shape item kept in x-graphvein, as stored.
type ShapeItem struct {
	Section string `json:"section"`
	// Row is the claim row that keeps the item out of the standard
	// fields; Why says it in words.
	Row  string `json:"row"`
	Why  string `json:"why"`
	Item any    `json:"item"`
}

// schemeEntry is one security scheme definition an operation uses.
type schemeEntry struct {
	def map[string]any
}

// ResponseDescription is the description of every written response
// (OG-80): OpenAPI requires the field, and Graphvein reads no prose.
const ResponseDescription = "Not read by Graphvein"

// componentName is the pattern OpenAPI requires of component keys.
var componentName = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// whyKind says why an item of a kind other than fact is not a standard
// field (OG-76, OG-77).
func whyKind(k routeshape.Kind) (string, string) {
	switch k {
	case routeshape.ReadInHandler:
		return "OG-76", "a name read in code: its type and whether it must be sent are not read"
	case routeshape.Intended:
		return "OG-77", "an intended type: a static type nothing checks at runtime"
	case routeshape.Returned:
		return "OG-77", "the declared type of a returned value, not validated by the framework"
	case routeshape.Mapped:
		return "OG-77", "from a committed spec or typed client linked to the route: shown beside the code, never merged"
	}
	return "OG-77", "kind " + string(k) + " is not a fact"
}

// reservedHeaders are header parameter names OpenAPI 3.1 says are ignored
// (they are described by content and security instead).
var reservedHeaders = map[string]bool{"accept": true, "content-type": true, "authorization": true}

// applyShape writes the route's stored shape into op (rows OG-70..OG-89).
// A route without a valid shape keeps OG-26..OG-29: nothing written but
// path parameter names.
func (g *generator) applyShape(op *Operation) {
	s := op.shape
	if s == nil {
		return
	}
	ext := &ShapeExt{Version: s.Version, DepthLimit: s.Follow.DepthLimit, Sections: map[string]SectionExt{}, NotStandard: []ShapeItem{},
		Models: s.Models, DepthCap: []routeshape.ChainRecord{}, Stops: map[string]int{}}
	op.X.Shape = ext
	sections := []struct {
		name string
		sec  routeshape.Section
	}{
		{routeshape.SectionPath, s.Path.Section}, {routeshape.SectionQuery, s.Query.Section}, {routeshape.SectionHeader, s.Header.Section},
		{routeshape.SectionCookie, s.Cookie.Section}, {routeshape.SectionBody, s.Body.Section}, {routeshape.SectionResponses, s.Responses.Section},
		{routeshape.SectionSecurity, s.Security.Section},
	}
	for _, sec := range sections {
		// A section whose items all come from a committed spec is not
		// read in the code (SH-O19): the document states the code's view,
		// and the mapped items stay in not_standard (OG-77).
		ext.Sections[sec.name] = SectionExt{State: string(sec.sec.CodeState()), Reason: sec.sec.Reason, Evidence: sec.sec.Evidence}
	}
	keep := func(section, row, why string, item any) {
		ext.NotStandard = append(ext.NotStandard, ShapeItem{Section: section, Row: row, Why: why, Item: item})
	}
	standard := func(section string) {
		e := ext.Sections[section]
		e.Standard++
		ext.Sections[section] = e
	}

	// Path parameters: the key's names, typed by a fact item (OG-72, OG-73).
	byName := map[string]int{}
	for i, p := range op.Parameters {
		byName[p.Name] = i
	}
	for _, item := range s.Path.Items {
		if item.Provenance.Kind != routeshape.Fact {
			row, why := whyKind(item.Provenance.Kind)
			keep(routeshape.SectionPath, row, why, item)
			continue
		}
		i, ok := byName[item.Name]
		if !ok || item.Access != routeshape.Declared {
			keep(routeshape.SectionPath, "OG-73", "a path parameter the path key does not hold", item)
			continue
		}
		p := &op.Parameters[i]
		if item.Schema != nil {
			schema, ok := g.convert(s, *item.Schema)
			if !ok {
				keep(routeshape.SectionPath, "OG-85", "its schema refers to a model that is not a fact", item)
				continue
			}
			p.Schema = schema
			delete(p.X, "type")
		}
		p.X["provenance"] = item.Provenance
		standard(routeshape.SectionPath)
	}

	// Query, header and cookie parameters (OG-74..OG-76).
	for _, sec := range []struct {
		name  string
		items []routeshape.Param
	}{{routeshape.SectionQuery, s.Query.Items}, {routeshape.SectionHeader, s.Header.Items}, {routeshape.SectionCookie, s.Cookie.Items}} {
		for _, item := range sec.items {
			if item.Provenance.Kind != routeshape.Fact {
				row, why := whyKind(item.Provenance.Kind)
				keep(sec.name, row, why, item)
				continue
			}
			switch {
			case item.Access != routeshape.Declared:
				keep(sec.name, "OG-76", "read in code, not a declared parameter", item)
				continue
			case item.Presence == routeshape.PresenceUnknown:
				keep(sec.name, "OG-75", "whether it must be sent is not read; OpenAPI would read a missing required as optional", item)
				continue
			case sec.name == routeshape.SectionHeader && reservedHeaders[strings.ToLower(item.Name)]:
				keep(sec.name, "OG-75", "OpenAPI ignores a header parameter named "+item.Name, item)
				continue
			}
			schema := map[string]any{}
			x := map[string]any{"provenance": item.Provenance}
			if item.Schema != nil {
				var ok bool
				if schema, ok = g.convert(s, *item.Schema); !ok {
					keep(sec.name, "OG-85", "its schema refers to a model that is not a fact", item)
					continue
				}
			} else {
				x["type"] = "not_read"
			}
			required := item.Presence == routeshape.Required
			op.Parameters = append(op.Parameters, Parameter{Name: item.Name, In: sec.name, Required: &required, Schema: schema, X: x})
			standard(sec.name)
		}
	}

	// Request body (OG-78, OG-79).
	for _, item := range s.Body.Items {
		if item.Provenance.Kind != routeshape.Fact {
			row, why := whyKind(item.Provenance.Kind)
			keep(routeshape.SectionBody, row, why, item)
			continue
		}
		if item.ContentType == nil {
			keep(routeshape.SectionBody, "OG-79", "its content type is not read; OpenAPI needs one", item)
			continue
		}
		mt := &MediaType{X: map[string]any{"provenance": item.Provenance}}
		if item.Schema != nil {
			schema, ok := g.convert(s, *item.Schema)
			if !ok {
				keep(routeshape.SectionBody, "OG-85", "its schema refers to a model that is not a fact", item)
				continue
			}
			mt.Schema = schema
		} else {
			mt.X["schema"] = "not_read"
		}
		if op.RequestBody == nil {
			op.RequestBody = &RequestBody{Content: map[string]*MediaType{}, X: map[string]any{"required": "not_read"}}
		}
		op.RequestBody.Content[*item.ContentType] = mt
		standard(routeshape.SectionBody)
	}

	// Responses (OG-80..OG-82).
	for _, item := range s.Responses.Items {
		if item.Provenance.Kind != routeshape.Fact {
			row, why := whyKind(item.Provenance.Kind)
			keep(routeshape.SectionResponses, row, why, item)
			continue
		}
		emptyBody := item.Schema != nil && item.Schema.Type == routeshape.TypeNull && !item.Schema.Nullable
		if item.ContentType == nil && !emptyBody {
			keep(routeshape.SectionResponses, "OG-82", "its content type is not read; OpenAPI needs one for a body", item)
			continue
		}
		var mt *MediaType
		if item.ContentType != nil {
			mt = &MediaType{X: map[string]any{"provenance": item.Provenance}}
			if item.Schema != nil {
				schema, ok := g.convert(s, *item.Schema)
				if !ok {
					keep(routeshape.SectionResponses, "OG-85", "its schema refers to a model that is not a fact", item)
					continue
				}
				mt.Schema = schema
			} else {
				mt.X["schema"] = "not_read"
			}
		}
		if op.Responses == nil {
			op.Responses = map[string]*Response{}
		}
		resp := op.Responses[item.Status]
		if resp == nil {
			resp = &Response{Description: ResponseDescription, X: map[string]any{"description": "not_read"}}
			op.Responses[item.Status] = resp
		}
		if mt == nil {
			resp.X["body"] = "none"
			resp.X["provenance"] = item.Provenance
		} else {
			if resp.Content == nil {
				resp.Content = map[string]*MediaType{}
			}
			resp.Content[*item.ContentType] = mt
		}
		standard(routeshape.SectionResponses)
	}

	// Security (OG-87, OG-88). Mapped alternatives are never part of
	// the code's requirement (SH-O19): they are kept beside it.
	var codeSecurity []routeshape.Requirement
	for _, item := range s.Security.Items {
		if item.Provenance.Kind == routeshape.Mapped {
			row, why := whyKind(item.Provenance.Kind)
			keep(routeshape.SectionSecurity, row, why, item)
			continue
		}
		codeSecurity = append(codeSecurity, item)
	}
	switch state := s.Security.CodeState(); state {
	case routeshape.NotRead:
	case routeshape.Complete:
		req, schemes, why := g.shapeSecurity(s, codeSecurity)
		if why != "" {
			for _, item := range codeSecurity {
				keep(routeshape.SectionSecurity, "OG-87", why, item)
			}
			break
		}
		op.security, op.schemes = req, schemes
		for range codeSecurity {
			standard(routeshape.SectionSecurity)
		}
	default:
		why := "the security section is " + string(state) + ": a requirement is written only when every alternative is known"
		if state == routeshape.ProvablyNone {
			why = "provably none: Graphvein never writes security: [] (a proxy or middleware outside the code may still require auth)"
		}
		for _, item := range codeSecurity {
			keep(routeshape.SectionSecurity, "OG-87", why, item)
		}
	}

	// Call following (OG-89).
	for _, c := range s.Follow.Chains {
		ext.Stops[string(c.Stop)]++
		if c.Stop == routeshape.StopDepthCap {
			ext.DepthCap = append(ext.DepthCap, c)
		}
	}
	sort.SliceStable(op.Parameters, func(i, j int) bool {
		return locationOrder[op.Parameters[i].In] < locationOrder[op.Parameters[j].In]
	})
}

var locationOrder = map[string]int{"path": 0, "query": 1, "header": 2, "cookie": 3}

// shapeSecurity turns a complete security section into one requirement
// list (OG-87): every alternative of kind fact and every scheme it names
// complete for its OpenAPI type. why says what refuses it.
func (g *generator) shapeSecurity(s *routeshape.Shape, items []routeshape.Requirement) ([]map[string][]string, map[string]schemeEntry, string) {
	defs := map[string]routeshape.Scheme{}
	for _, sc := range s.Schemes {
		defs[sc.Name] = sc
	}
	var req []map[string][]string
	used := map[string]schemeEntry{}
	for _, item := range items {
		if item.Provenance.Kind != routeshape.Fact {
			_, why := whyKind(item.Provenance.Kind)
			return nil, nil, "an alternative is not a fact (" + why + ")"
		}
		alt := map[string][]string{}
		for _, use := range item.Schemes {
			sc := defs[use.Name]
			def, why := schemeObject(sc)
			if why != "" {
				return nil, nil, "scheme " + use.Name + ": " + why
			}
			if !componentName.MatchString(use.Name) {
				return nil, nil, "scheme name " + use.Name + " is not a valid OpenAPI component name"
			}
			used[use.Name] = schemeEntry{def: def}
			alt[use.Name] = append([]string{}, use.Scopes...)
		}
		req = append(req, alt)
	}
	return req, used, ""
}

// schemeObject is the OpenAPI security scheme of a stored scheme, or why
// it is incomplete (OG-87).
func schemeObject(sc routeshape.Scheme) (map[string]any, string) {
	if sc.Provenance.Kind != routeshape.Fact {
		return nil, "not a fact"
	}
	x := map[string]any{"provenance": sc.Provenance}
	switch sc.Type {
	case routeshape.SchemeHTTP:
		if sc.Scheme == nil {
			return nil, "its HTTP scheme is not read"
		}
		def := map[string]any{"type": "http", "scheme": *sc.Scheme, "x-graphvein": x}
		if sc.BearerFormat != nil {
			def["bearerFormat"] = *sc.BearerFormat
		}
		return def, ""
	case routeshape.SchemeAPIKey:
		if sc.In == nil || sc.ParamName == nil {
			return nil, "where the key is sent or its name is not read"
		}
		return map[string]any{"type": "apiKey", "in": *sc.In, "name": *sc.ParamName, "x-graphvein": x}, ""
	case routeshape.SchemeOpenIDConnect:
		if sc.OpenIDConnectURL == nil {
			return nil, "its discovery URL is not read"
		}
		return map[string]any{"type": "openIdConnect", "openIdConnectUrl": *sc.OpenIDConnectURL, "x-graphvein": x}, ""
	case routeshape.SchemeOAuth2:
		return nil, "OAuth2 needs its flows; the stored token URL does not say which flow"
	}
	return nil, "its type is not read"
}

// httpSchemes are the FastAPI scheme classes whose OpenAPI object is
// complete from the class alone (OG-31).
var httpSchemes = map[string]string{"HTTPBearer": "bearer", "HTTPBasic": "basic", "HTTPDigest": "digest"}

// attributeSecurity applies OG-30 and OG-31 to a route without a read
// shape security section: one requirement naming every stored FastAPI
// security dependency, only when all are known, complete HTTP schemes and
// auto_error is literally true.
func (g *generator) attributeSecurity(op *Operation) {
	attrs := op.route.attrs
	deps := objects(attrs["security_dependencies"])
	if len(deps) == 0 {
		return
	}
	if v, ok := attrs["security_dependencies_unknown"]; ok {
		if list, _ := v.([]any); len(list) > 0 || list == nil {
			return
		}
	}
	alt := map[string][]string{}
	used := map[string]schemeEntry{}
	for _, d := range deps {
		if sub, ok := d["subclass"]; ok && sub != nil {
			// A repository subclass may override __call__ (OG-31).
			return
		}
		class, _ := d["class"].(string)
		name, _ := d["scheme_name"].(string)
		auto, ok := d["auto_error"].(bool)
		scheme := httpSchemes[class]
		if scheme == "" || !ok || !auto || !componentName.MatchString(name) {
			return
		}
		used[name] = schemeEntry{def: map[string]any{"type": "http", "scheme": scheme, "x-graphvein": map[string]any{"class": class, "rule": "OG-31"}}}
		alt[name] = []string{}
	}
	op.security, op.schemes = []map[string][]string{alt}, used
}

// finishSecurity writes each pending requirement unless a scheme name it
// uses has two definitions in the document (OG-31: neither is written),
// then fills every operation's not_read list (OG-41).
func (g *generator) finishSecurity() {
	defs := map[string]map[string]map[string]any{}
	users := map[string][]string{}
	g.eachOperation(func(key, method string, op *Operation) {
		for name, e := range op.schemes {
			if defs[name] == nil {
				defs[name] = map[string]map[string]any{}
			}
			defs[name][schemeKey(e.def)] = e.def
			users[name] = append(users[name], op.route.id)
		}
	})
	conflicting := map[string]bool{}
	var names []string
	for name := range defs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if len(defs[name]) > 1 {
			conflicting[name] = true
			g.conflicts = append(g.conflicts, Conflict{Reason: ConflictScheme, Keys: []string{name}, Routes: sortedDistinct(users[name])})
		}
	}
	g.schemes = map[string]map[string]schemeEntry{}
	g.eachOperation(func(key, method string, op *Operation) {
		ok := len(op.security) > 0
		for name := range op.schemes {
			if conflicting[name] {
				ok = false
			}
		}
		if ok {
			op.Security = op.security
			for name, e := range op.schemes {
				if g.schemes[name] == nil {
					g.schemes[name] = map[string]schemeEntry{}
				}
				if _, seen := g.schemes[name][schemeKey(e.def)]; !seen {
					g.schemes[name][schemeKey(e.def)] = e
				}
			}
		}
		op.X.NotRead = notRead(op)
	})
}

// schemeKey compares scheme definitions by their OpenAPI fields: the
// same scheme read from a shape and from FastAPI's stored class differs
// only in x-graphvein, and is one scheme.
func schemeKey(def map[string]any) string {
	plain := map[string]any{}
	for k, v := range def {
		if k != "x-graphvein" {
			plain[k] = v
		}
	}
	return canonical(plain)
}

// registeredSchemes are the security schemes written operations use.
func (g *generator) registeredSchemes() map[string]map[string]any {
	if len(g.schemes) == 0 {
		return nil
	}
	out := map[string]map[string]any{}
	for name, defs := range g.schemes {
		for _, e := range defs {
			out[name] = e.def
		}
	}
	return out
}

// eachOperation calls f for every operation in path key and method order.
func (g *generator) eachOperation(f func(key, method string, op *Operation)) {
	keys := make([]string, 0, len(g.doc.Paths))
	for k := range g.doc.Paths {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, m := range methodOrder {
			if op := *g.doc.Paths[k].slot(m); op != nil {
				f(k, m, op)
			}
		}
	}
}

// convert turns a stored schema of a fact item into a JSON Schema
// (OG-83..OG-86). It refuses (false) a schema that refers, directly or
// through models, to a model that is not a fact.
func (g *generator) convert(s *routeshape.Shape, sc routeshape.Schema) (map[string]any, bool) {
	out := map[string]any{}
	switch sc.Type {
	case routeshape.TypeObject:
		out["type"] = "object"
		props := map[string]any{}
		required := []string{}
		for _, f := range sc.Fields {
			fs, ok := g.convert(s, f.Schema)
			if !ok {
				return nil, false
			}
			switch f.Presence {
			case routeshape.Required:
				required = append(required, f.Name)
			case routeshape.PresenceUnknown:
				addX(fs, "presence", "unknown")
			}
			props[f.Name] = fs
		}
		if len(props) > 0 {
			out["properties"] = props
		}
		if len(required) > 0 {
			out["required"] = required
		}
		switch sc.Additional {
		case routeshape.AdditionalForbidden:
			out["additionalProperties"] = false
		case routeshape.AdditionalAllowed:
		default:
			addX(out, "additional", "unknown")
		}
	case routeshape.TypeArray:
		if sc.Items == nil {
			return nil, false
		}
		items, ok := g.convert(s, *sc.Items)
		if !ok {
			return nil, false
		}
		out["type"], out["items"] = "array", items
	case routeshape.TypeString, routeshape.TypeNumber, routeshape.TypeInteger, routeshape.TypeBoolean, routeshape.TypeNull:
		out["type"] = string(sc.Type)
	case routeshape.TypeEnum:
		out["enum"] = append([]any{}, sc.Enum...)
	case routeshape.TypeRef:
		key, ok := g.models.register(g, s, sc.Ref)
		if !ok {
			return nil, false
		}
		out["$ref"] = "#/components/schemas/" + key
	default:
		addX(out, "unknown", sc.Reason)
	}
	if sc.Nullable {
		switch sc.Type {
		case routeshape.TypeEnum:
			hasNull := false
			for _, v := range sc.Enum {
				hasNull = hasNull || v == nil
			}
			if !hasNull {
				out["enum"] = append(out["enum"].([]any), nil)
			}
		case routeshape.TypeRef:
			out = map[string]any{"anyOf": []any{out, map[string]any{"type": "null"}}}
		case routeshape.TypeUnknown, routeshape.TypeNull:
		default:
			out["type"] = []any{out["type"], "null"}
		}
	}
	return out, true
}

// addX sets one x-graphvein field of a schema.
func addX(schema map[string]any, key string, value any) {
	x, _ := schema["x-graphvein"].(map[string]any)
	if x == nil {
		x = map[string]any{}
		schema["x-graphvein"] = x
	}
	x[key] = value
}

// modelRegistry names the fact models written to components.schemas
// (OG-85). Two routes whose models share a name and a definition share
// one component; a different definition under the same name gets the
// name with a numeric suffix, and its x-graphvein names the model.
type modelRegistry struct {
	byName  map[string][]modelDef
	schemas map[string]map[string]any
	taken   map[string]bool
}

type modelDef struct{ canon, key string }

func newModelRegistry() *modelRegistry {
	return &modelRegistry{byName: map[string][]modelDef{}, schemas: map[string]map[string]any{}, taken: map[string]bool{}}
}

func (m *modelRegistry) empty() bool { return len(m.schemas) == 0 }

func (m *modelRegistry) components() map[string]map[string]any {
	if len(m.schemas) == 0 {
		return nil
	}
	return m.schemas
}

// register returns the component key of model name of s, writing the
// component the first time; false when the model is missing or (through
// its references) not a fact.
func (m *modelRegistry) register(g *generator, s *routeshape.Shape, name string) (string, bool) {
	model, ok := findModel(s, name)
	if !ok || !allFacts(s, name, map[string]bool{}) {
		return "", false
	}
	canon := closure(s, name, map[string]bool{})
	for _, d := range m.byName[name] {
		if d.canon == canon {
			return d.key, true
		}
	}
	base := sanitize(name)
	key := base
	for n := 2; m.taken[key]; n++ {
		key = fmt.Sprintf("%s_%d", base, n)
	}
	m.taken[key] = true
	m.byName[name] = append(m.byName[name], modelDef{canon: canon, key: key})
	// A placeholder first: a model that refers to itself resolves to key.
	m.schemas[key] = map[string]any{}
	schema, ok := g.convert(s, model.Schema)
	if !ok {
		delete(m.schemas, key)
		return "", false
	}
	addX(schema, "model", name)
	addX(schema, "provenance", model.Provenance)
	m.schemas[key] = schema
	return key, true
}

func findModel(s *routeshape.Shape, name string) (routeshape.Model, bool) {
	for _, md := range s.Models {
		if md.Name == name {
			return md, true
		}
	}
	return routeshape.Model{}, false
}

// refs lists the model names a schema refers to, in schema order.
func refs(sc routeshape.Schema, out *[]string) {
	switch sc.Type {
	case routeshape.TypeRef:
		*out = append(*out, sc.Ref)
	case routeshape.TypeArray:
		if sc.Items != nil {
			refs(*sc.Items, out)
		}
	case routeshape.TypeObject:
		for _, f := range sc.Fields {
			refs(f.Schema, out)
		}
	}
}

// allFacts reports whether name and every model it reaches are facts.
func allFacts(s *routeshape.Shape, name string, seen map[string]bool) bool {
	if seen[name] {
		return true
	}
	seen[name] = true
	md, ok := findModel(s, name)
	if !ok || md.Provenance.Kind != routeshape.Fact {
		return false
	}
	var names []string
	refs(md.Schema, &names)
	for _, n := range names {
		if !allFacts(s, n, seen) {
			return false
		}
	}
	return true
}

// closure is the canonical text of a model with every model it reaches,
// so two shapes' models compare equal only when the whole definition is.
func closure(s *routeshape.Shape, name string, seen map[string]bool) string {
	if seen[name] {
		return "ref:" + name
	}
	seen[name] = true
	md, _ := findModel(s, name)
	var b strings.Builder
	b.WriteString(name + "=" + canonical(md.Schema))
	var names []string
	refs(md.Schema, &names)
	for _, n := range names {
		b.WriteString(";" + closure(s, n, seen))
	}
	return b.String()
}

// sanitize makes a component key from a model name.
func sanitize(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r < 128 && (r == '.' || r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "model"
	}
	return b.String()
}

package routes_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
)

// TestFastAPIShapeOracle compares the stored route shapes of the
// route-shape-fastapi fixture with FastAPI's own app.openapi()
// (scripts/oracle/fastapi_shape.sh). Opt-in, never run by CI:
//
//   - GRAPHVEIN_FASTAPI_SHAPE_ROOT: the scratch copy the oracle imported;
//   - GRAPHVEIN_FASTAPI_SHAPE_OPENAPI: the oracle's JSON output.
//
// Corpus mode compares a committed FastAPI spec (wrapped as
// {"openapi": ...}) with an index run's NDJSON instead, never running
// corpus code: GRAPHVEIN_FASTAPI_SHAPE_NDJSON names the NDJSON (the root
// is then unused); spec operations without a route are counted.
//
// Per operation it compares the parameters (name, location, required,
// schema type), the request body (content type, schema reference or
// fields), the response statuses and schemas, and the security schemes.
// A disagreement is a claim Graphvein states that FastAPI contradicts, or
// a section Graphvein states complete or provably_none that misses what
// FastAPI lists (a precision stop). What Graphvein leaves unclaimed
// (partial, not read) is counted, never a disagreement. Statuses from
// raised HTTPExceptions (row SH-F29) and template-only path parameters
// (row SH-F2) are not in FastAPI's spec by design and are counted apart.
func TestFastAPIShapeOracle(t *testing.T) {
	root, out := os.Getenv("GRAPHVEIN_FASTAPI_SHAPE_ROOT"), os.Getenv("GRAPHVEIN_FASTAPI_SHAPE_OPENAPI")
	if root == "" || out == "" {
		t.Skip("set GRAPHVEIN_FASTAPI_SHAPE_ROOT and GRAPHVEIN_FASTAPI_SHAPE_OPENAPI (scripts/oracle/fastapi_shape.sh)")
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var oracle struct {
		FastAPI  string         `json:"fastapi"`
		Pydantic string         `json:"pydantic"`
		OpenAPI  map[string]any `json:"openapi"`
	}
	if err := json.Unmarshal(data, &oracle); err != nil {
		t.Fatal(err)
	}
	var nodes []contract.Node
	corpus := os.Getenv("GRAPHVEIN_FASTAPI_SHAPE_NDJSON")
	if corpus != "" {
		nodes = readRouteNodes(t, corpus)
	} else {
		snapshot, _ := indexShapeFixture(t, root, 0)
		nodes = snapshot.Nodes
	}
	c := &shapeComparison{doc: oracle.OpenAPI, counts: map[string]int{}}
	compared := map[string]bool{}
	paths, _ := oracle.OpenAPI["paths"].(map[string]any)
	for _, n := range nodes {
		if n.Kind != "route" || n.Attributes[routeshape.AttributeKey] == nil {
			continue
		}
		shape, err := routeshape.FromAttribute(n.Attributes[routeshape.AttributeKey])
		if err != nil {
			t.Fatalf("%s: %v", n.Identity, err)
		}
		method, _ := n.Attributes["method"].(string)
		entries, _ := n.Attributes["paths"].([]any)
		for _, e := range entries {
			entry, _ := e.(map[string]any)
			template, _ := entry["template"].(string)
			if entry["trailing_slash"] == true {
				template += "/"
			}
			item, _ := paths[template].(map[string]any)
			op, ok := item[strings.ToLower(method)].(map[string]any)
			if !ok {
				c.counts["routes not in the spec (include_in_schema=False)"]++
				continue
			}
			key := method + " " + template
			compared[key] = true
			c.operation(key, op, shape)
		}
	}
	// unmounted are the routes stored without a path (a prefix not read,
	// e.g. a router constructed with keyword unpacking): method and
	// pattern.
	var unmounted []string
	for _, n := range nodes {
		if entries, _ := n.Attributes["paths"].([]any); n.Kind == "route" && len(entries) == 0 {
			method, _ := n.Attributes["method"].(string)
			pattern, _ := n.Attributes["pattern"].(string)
			unmounted = append(unmounted, method+" "+pattern)
		}
	}
	for path, item := range paths {
		for method := range item.(map[string]any) {
			if !compared[strings.ToUpper(method)+" "+path] {
				if corpus != "" {
					// A committed spec may be older than the code.
					c.counts["spec operations without a route"]++
					continue
				}
				if slices.ContainsFunc(unmounted, func(u string) bool {
					m, pattern, _ := strings.Cut(u, " ")
					return m == strings.ToUpper(method) && strings.HasSuffix(path, pattern)
				}) {
					c.counts["spec operations of routes stored without a path (prefix not read)"]++
					continue
				}
				t.Errorf("operation %s %s has no stored route shape", method, path)
			}
		}
	}
	for _, d := range c.disagreements {
		t.Error(d)
	}
	var keys []string
	for k := range c.counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Logf("%s: %d", k, c.counts[k])
	}
	t.Logf("fastapi %s, pydantic %s: %d operations, %d disagreements", oracle.FastAPI, oracle.Pydantic, len(compared), len(c.disagreements))
}

type shapeComparison struct {
	doc           map[string]any
	counts        map[string]int
	disagreements []string
}

func (c *shapeComparison) disagree(key, format string, args ...any) {
	c.disagreements = append(c.disagreements, key+": "+fmt.Sprintf(format, args...))
}

// component resolves a $ref to its schema.
func (c *shapeComparison) component(ref string) map[string]any {
	name := ref[strings.LastIndex(ref, "/")+1:]
	comps, _ := c.doc["components"].(map[string]any)
	schemas, _ := comps["schemas"].(map[string]any)
	s, _ := schemas[name].(map[string]any)
	return s
}

// modelComponent finds a model's component: FastAPI may split one model
// into -Input and -Output schemas.
func (c *shapeComparison) modelComponents(name string) []map[string]any {
	var out []map[string]any
	for _, n := range []string{name, name + "-Input", name + "-Output"} {
		if s := c.component("#/components/schemas/" + n); s != nil {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		// A clash-qualified name: compare when exactly one spec component
		// has the class name.
		if found := c.byBase(modelBase(name)); len(found) == 1 {
			return found
		}
	}
	return out
}

// match reports whether our schema s agrees with the spec schema o; an
// unknown schema states nothing and agrees with anything.
func (c *shapeComparison) match(s routeshape.Schema, o map[string]any, models map[string]routeshape.Model) (bool, string) {
	if s.Type == routeshape.TypeUnknown {
		c.counts["schemas left unknown"]++
		return true, ""
	}
	if alts, ok := o["anyOf"].([]any); ok {
		var rest []map[string]any
		null := false
		for _, a := range alts {
			m, _ := a.(map[string]any)
			if m["type"] == "null" {
				null = true
				continue
			}
			rest = append(rest, m)
		}
		if len(rest) != 1 {
			return false, "spec has a union"
		}
		if null != s.Nullable {
			return false, fmt.Sprintf("nullable %v, spec %v", s.Nullable, null)
		}
		s.Nullable = false
		o = rest[0]
	} else if s.Nullable {
		return false, "nullable, spec is not"
	}
	if ref, ok := o["$ref"].(string); ok {
		comp := c.component(ref)
		name := ref[strings.LastIndex(ref, "/")+1:]
		if s.Type == routeshape.TypeRef {
			if modelBase(s.Ref) != modelBase(name) {
				return false, fmt.Sprintf("ref %s, spec %s", s.Ref, name)
			}
			if s.Ref != strings.TrimSuffix(strings.TrimSuffix(name, "-Input"), "-Output") {
				c.counts["model names qualified differently (a name clash)"]++
			}
			return true, ""
		}
		if comp == nil {
			return false, "spec ref " + ref + " not found"
		}
		return c.match(s, comp, models)
	}
	typ, _ := o["type"].(string)
	switch s.Type {
	case routeshape.TypeRef:
		if typ == "object" {
			if m, ok := models[s.Ref]; ok {
				return c.match(m.Schema, o, models)
			}
		}
		return false, fmt.Sprintf("ref %s, spec type %q", s.Ref, typ)
	case routeshape.TypeEnum:
		values, ok := o["enum"].([]any)
		if cv, isConst := o["const"]; isConst && !ok {
			values, ok = []any{cv}, true
		}
		if !ok {
			return false, "enum, spec has no enum"
		}
		var a, b []string
		for _, v := range values {
			a = append(a, fmt.Sprint(v))
		}
		for _, v := range s.Enum {
			b = append(b, fmt.Sprint(v))
		}
		sort.Strings(a)
		sort.Strings(b)
		if !reflect.DeepEqual(a, b) {
			return false, fmt.Sprintf("enum %v, spec %v", b, a)
		}
		return true, ""
	case routeshape.TypeArray:
		if typ != "array" {
			return false, fmt.Sprintf("array, spec %q", typ)
		}
		items, _ := o["items"].(map[string]any)
		return c.match(*s.Items, items, models)
	case routeshape.TypeObject:
		if typ != "object" {
			return false, fmt.Sprintf("object, spec %q", typ)
		}
		if len(s.Fields) == 0 {
			return true, ""
		}
		return c.fields(s, o, models)
	case routeshape.TypeNull:
		return typ == "null", "null"
	}
	if string(s.Type) != typ {
		return false, fmt.Sprintf("type %s, spec %q", s.Type, typ)
	}
	return true, ""
}

// fields compares an object's fields with the spec's properties.
func (c *shapeComparison) fields(s routeshape.Schema, o map[string]any, models map[string]routeshape.Model) (bool, string) {
	props, _ := o["properties"].(map[string]any)
	required := map[string]bool{}
	if req, ok := o["required"].([]any); ok {
		for _, r := range req {
			required[r.(string)] = true
		}
	}
	if len(props) != len(s.Fields) {
		return false, fmt.Sprintf("%d fields, spec %d", len(s.Fields), len(props))
	}
	for _, f := range s.Fields {
		p, ok := props[f.Name].(map[string]any)
		if !ok {
			return false, "field " + f.Name + " not in the spec"
		}
		if f.Presence != routeshape.PresenceUnknown && (f.Presence == routeshape.Required) != required[f.Name] {
			return false, fmt.Sprintf("field %s presence %s, spec required %v", f.Name, f.Presence, required[f.Name])
		}
		if ok, why := c.match(f.Schema, p, models); !ok {
			return false, "field " + f.Name + ": " + why
		}
	}
	return true, ""
}

func (c *shapeComparison) operation(key string, op map[string]any, sh *routeshape.Shape) {
	models := map[string]routeshape.Model{}
	for _, m := range sh.Models {
		models[m.Name] = m
		if m.Schema.Type != routeshape.TypeObject || m.Name == "HTTPValidationError" || m.Name == "ValidationError" {
			continue
		}
		comps := c.modelComponents(m.Name)
		if len(comps) == 0 {
			if modelBase(m.Name) != m.Name || len(c.byBase(m.Name)) > 1 {
				// FastAPI qualifies clashing names its own way.
				c.counts["clash-qualified models not compared"]++
				continue
			}
			c.disagree(key, "model %s is not a spec component", m.Name)
			continue
		}
		for _, comp := range comps {
			if ok, why := c.fields(m.Schema, comp, models); !ok {
				c.disagree(key, "model %s: %s", m.Name, why)
			}
		}
		c.counts["model fields compared"] += len(m.Schema.Fields)
	}
	c.params(key, op, sh, models)
	c.body(key, op, sh, models)
	c.responses(key, op, sh, models)
	c.security(key, op, sh)
}

func (c *shapeComparison) params(key string, op map[string]any, sh *routeshape.Shape, models map[string]routeshape.Model) {
	sections := map[string]*routeshape.ParamSection{"path": &sh.Path, "query": &sh.Query, "header": &sh.Header, "cookie": &sh.Cookie}
	spec := map[string]map[string]any{}
	params, _ := op["parameters"].([]any)
	for _, p := range params {
		m := p.(map[string]any)
		in, name := m["in"].(string), m["name"].(string)
		if in == "header" {
			name = strings.ToLower(name)
		}
		spec[in+" "+name] = m
	}
	stated := map[string]bool{}
	for in, sec := range sections {
		for _, it := range sec.Items {
			if it.Access != routeshape.Declared || it.Provenance.Kind != routeshape.Fact {
				c.counts["parameters read in code (not in a spec)"]++
				continue
			}
			name := it.Name
			if in == "header" {
				name = strings.ToLower(name)
			}
			if it.Provenance.Rule == "SH-F2" {
				if _, ok := spec[in+" "+name]; !ok {
					c.counts["template-only path parameters (FastAPI's spec omits them)"]++
					continue
				}
			}
			stated[in+" "+name] = true
			m, ok := spec[in+" "+name]
			if !ok {
				c.disagree(key, "states %s parameter %s, not in the spec", in, it.Name)
				continue
			}
			if (it.Presence == routeshape.Required) != (m["required"] == true) {
				c.disagree(key, "%s parameter %s presence %s, spec required %v", in, it.Name, it.Presence, m["required"])
				continue
			}
			if it.Schema != nil {
				schema, _ := m["schema"].(map[string]any)
				if ok, why := c.match(*it.Schema, schema, models); !ok {
					c.disagree(key, "%s parameter %s: %s", in, it.Name, why)
					continue
				}
			}
			c.counts["parameters agree"]++
		}
	}
	for k := range spec {
		if stated[k] {
			continue
		}
		in := strings.SplitN(k, " ", 2)[0]
		switch sections[in].State {
		case routeshape.Complete, routeshape.ProvablyNone:
			c.disagree(key, "section %s is %s but misses %s", in, sections[in].State, k)
		default:
			c.counts["spec parameters not claimed (section partial or not read)"]++
		}
	}
}

func (c *shapeComparison) body(key string, op map[string]any, sh *routeshape.Shape, models map[string]routeshape.Model) {
	rb, has := op["requestBody"].(map[string]any)
	var facts []routeshape.Content
	for _, it := range sh.Body.Items {
		if it.Provenance.Kind == routeshape.Fact {
			facts = append(facts, it)
		}
	}
	if !has {
		if len(facts) > 0 {
			c.disagree(key, "states a body, the spec has none")
			return
		}
		c.counts["no body agrees"]++
		return
	}
	content, _ := rb["content"].(map[string]any)
	if len(facts) == 0 {
		if sh.Body.State == routeshape.ProvablyNone || sh.Body.State == routeshape.Complete {
			c.disagree(key, "body %s, the spec has one", sh.Body.State)
			return
		}
		c.counts["spec bodies not claimed"]++
		return
	}
	for _, f := range facts {
		ct := *f.ContentType
		media, ok := content[ct].(map[string]any)
		if !ok {
			c.disagree(key, "body content type %s, spec %v", ct, keysOf(content))
			continue
		}
		schema, _ := media["schema"].(map[string]any)
		if f.Schema != nil {
			if ok, why := c.match(*f.Schema, schema, models); !ok {
				c.disagree(key, "body: %s", why)
				continue
			}
		}
		c.counts["bodies agree"]++
	}
}

func keysOf(m map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (c *shapeComparison) responses(key string, op map[string]any, sh *routeshape.Shape, models map[string]routeshape.Model) {
	spec, _ := op["responses"].(map[string]any)
	stated := map[string]bool{}
	for _, r := range sh.Responses.Items {
		if r.Provenance.Rule == "SH-F29" {
			c.counts["statuses from raised HTTPException (not in a spec)"]++
			continue
		}
		if r.Provenance.Rule == "SH-F31" {
			c.counts["returned types followed (beyond the spec)"]++
			continue
		}
		stated[r.Status] = true
		o, ok := spec[r.Status].(map[string]any)
		if !ok {
			c.disagree(key, "states status %s, not in the spec %v", r.Status, keysOf(spec))
			continue
		}
		content, _ := o["content"].(map[string]any)
		if r.Schema != nil && r.Schema.Type == routeshape.TypeNull {
			if len(content) > 0 {
				c.disagree(key, "status %s without a body, the spec has content", r.Status)
				continue
			}
			c.counts["responses agree"]++
			continue
		}
		if r.ContentType != nil {
			media, ok := content[*r.ContentType].(map[string]any)
			if !ok {
				if r.Provenance.Rule == "SH-F28" && r.Schema == nil {
					c.counts["responses agree"]++
					continue
				}
				c.disagree(key, "status %s content type %s, spec %v", r.Status, *r.ContentType, keysOf(content))
				continue
			}
			schema, _ := media["schema"].(map[string]any)
			if r.Schema != nil {
				if ok, why := c.match(*r.Schema, schema, models); !ok {
					c.disagree(key, "status %s: %s", r.Status, why)
					continue
				}
			}
		}
		c.counts["responses agree"]++
	}
	for status := range spec {
		if !stated[status] {
			c.counts["spec statuses not claimed"]++
		}
	}
}

func (c *shapeComparison) security(key string, op map[string]any, sh *routeshape.Shape) {
	spec := map[string]bool{}
	reqs, _ := op["security"].([]any)
	for _, r := range reqs {
		for name := range r.(map[string]any) {
			spec[name] = true
		}
	}
	stated := map[string]bool{}
	for _, r := range sh.Security.Items {
		for _, u := range r.Schemes {
			stated[u.Name] = true
			if !spec[u.Name] {
				c.disagree(key, "states scheme %s, not in the operation's security", u.Name)
			}
		}
	}
	for name := range spec {
		if !stated[name] {
			if sh.Security.State == routeshape.Complete || sh.Security.State == routeshape.ProvablyNone {
				c.disagree(key, "security %s misses %s", sh.Security.State, name)
			}
			c.counts["spec security requirements not claimed"]++
		} else {
			c.counts["security requirements agree"]++
		}
	}
	comps, _ := c.doc["components"].(map[string]any)
	schemes, _ := comps["securitySchemes"].(map[string]any)
	for _, s := range sh.Schemes {
		o, ok := schemes[s.Name].(map[string]any)
		if !ok {
			c.disagree(key, "scheme %s is not a spec security scheme", s.Name)
			continue
		}
		got := map[string]any{"type": string(s.Type)}
		want := map[string]any{"type": o["type"]}
		set := func(k string, v *string, ov any) {
			if v != nil {
				got[k], want[k] = *v, ov
			}
		}
		set("scheme", s.Scheme, o["scheme"])
		set("bearerFormat", s.BearerFormat, o["bearerFormat"])
		set("in", s.In, o["in"])
		set("name", s.ParamName, o["name"])
		set("openIdConnectUrl", s.OpenIDConnectURL, o["openIdConnectUrl"])
		if s.TokenURL != nil {
			flows, _ := o["flows"].(map[string]any)
			password, _ := flows["password"].(map[string]any)
			got["tokenUrl"], want["tokenUrl"] = *s.TokenURL, password["tokenUrl"]
		}
		if !reflect.DeepEqual(got, want) {
			c.disagree(key, "scheme %s: %v, spec %v", s.Name, got, want)
			continue
		}
		c.counts["scheme fields compared"] += len(got)
	}
}

// readRouteNodes reads the route nodes of an NDJSON snapshot.
func readRouteNodes(t *testing.T, path string) []contract.Node {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []contract.Node
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		if !bytes.Contains(sc.Bytes(), []byte(`"kind":"route"`)) {
			continue
		}
		var n contract.Node
		if err := json.Unmarshal(sc.Bytes(), &n); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// modelBase is a model's class name without a clash qualifier: ours is
// Name__file, FastAPI's module__path__Name, with -Input/-Output suffixes.
func modelBase(name string) string {
	name = strings.TrimSuffix(strings.TrimSuffix(name, "-Input"), "-Output")
	if i := strings.Index(name, "__"); i > 0 && name[:1] >= "A" && name[:1] <= "Z" {
		return name[:i]
	}
	if i := strings.LastIndex(name, "__"); i >= 0 {
		return name[i+2:]
	}
	return name
}

// byBase lists the spec components whose class name is base.
func (c *shapeComparison) byBase(base string) []map[string]any {
	comps, _ := c.doc["components"].(map[string]any)
	schemas, _ := comps["schemas"].(map[string]any)
	var out []map[string]any
	for name, s := range schemas {
		if modelBase(name) == base && !strings.HasSuffix(name, "-Output") {
			out = append(out, s.(map[string]any))
		}
	}
	return out
}

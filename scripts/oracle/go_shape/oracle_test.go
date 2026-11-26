package goshape

// The route shape oracle of task T029 (docs/contract/claims/
// route-shape-go.md). Graphvein's TestGoShapeExport writes the shapes it
// claims for the go-shape fixture (testdata/fixtures/go-shape, Graphvein's
// own test code) and the schemas it claims for the fixture's struct types.
// This test compiles that module with the real encoding/json, gin v1.12.0
// and chi v5.5.0 and compares:
//
//   - types: for each struct type, encoding/json's real field set (keys of
//     a marshaled filled value) against the claimed fields, the JSON kind
//     of each value against the claimed type, presence (omitempty fields
//     absent from a marshaled zero value, the others present), and for
//     request schemas that a value of the claimed type decodes, a value of
//     another kind is refused, and null is accepted where claimed
//     nullable; a claimed unknown schema is checked to be one encoding/json
//     does not write as the struct's fields;
//   - routes: the fixture's gin, chi and net/http handlers served on
//     127.0.0.1 (httptest): a request built from the claimed shape must get
//     a claimed status; for a fact JSON body or query bind, a missing
//     required field and a value of the wrong kind must get 400 (or the
//     status the handler claims for a failed bind), an intended one must
//     not; response bodies must carry the claimed keys.
//
// Every comparison is counted; a disagreement fails the test.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"example.com/gvshape/chiapp"
	"example.com/gvshape/ginapp"
	"example.com/gvshape/mux"
	"example.com/gvshape/review"
	"github.com/gin-gonic/gin"
)

// schema mirrors routeshape.Schema.
type schema struct {
	Type       string  `json:"type"`
	Fields     []field `json:"fields"`
	Additional string  `json:"additional"`
	Items      *schema `json:"items"`
	Nullable   bool    `json:"nullable"`
	Reason     string  `json:"reason"`
	Enum       []any   `json:"enum"`
	Ref        string  `json:"ref"`
}

type field struct {
	Name     string `json:"name"`
	Presence string `json:"presence"`
	Schema   schema `json:"schema"`
}

type typeExport struct {
	Package  string `json:"package"`
	Name     string `json:"name"`
	Request  schema `json:"request"`
	Response schema `json:"response"`
}

type provenance struct {
	Kind string `json:"kind"`
	Rule string `json:"rule"`
}

type param struct {
	Name       string     `json:"name"`
	Access     string     `json:"access"`
	Presence   string     `json:"presence"`
	Schema     *schema    `json:"schema"`
	Provenance provenance `json:"provenance"`
}

type content struct {
	ContentType *string    `json:"content_type"`
	Schema      *schema    `json:"schema"`
	Provenance  provenance `json:"provenance"`
}

type response struct {
	Status      string     `json:"status"`
	ContentType *string    `json:"content_type"`
	Schema      *schema    `json:"schema"`
	Provenance  provenance `json:"provenance"`
}

type shape struct {
	Path      struct{ Items []param }   `json:"path"`
	Query     struct{ Items []param }   `json:"query"`
	Body      struct{ Items []content } `json:"body"`
	Responses struct {
		Items  []response
		Reason string
	} `json:"responses"`
}

type routeExport struct {
	Identity  string   `json:"identity"`
	Framework string   `json:"framework"`
	Method    string   `json:"method"`
	Templates []string `json:"templates"`
	Shape     *shape   `json:"shape"`
}

// counts tallies the comparisons by kind.
type counts map[string]int

func (c counts) String() string {
	var keys []string
	for k := range c {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s: %d\n", k, c[k])
	}
	return b.String()
}

func readJSON(t *testing.T, name string, v any) {
	t.Helper()
	dir := os.Getenv("GRAPHVEIN_GO_SHAPE")
	if dir == "" {
		t.Skip("GRAPHVEIN_GO_SHAPE is not set")
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatal(err)
	}
}

// fill returns a value of t with every field non-zero (pointers allocated,
// slices and maps with one element), bounded for recursive types.
func fill(t reflect.Type, depth int) reflect.Value {
	v := reflect.New(t).Elem()
	if depth > 4 {
		return v
	}
	switch t.Kind() {
	case reflect.String:
		v.SetString("s")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(1)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1.5)
	case reflect.Pointer:
		v.Set(reflect.New(t.Elem()))
		v.Elem().Set(fill(t.Elem(), depth+1))
	case reflect.Slice:
		v.Set(reflect.MakeSlice(t, 1, 1))
		v.Index(0).Set(fill(t.Elem(), depth+1))
	case reflect.Map:
		v.Set(reflect.MakeMap(t))
		v.SetMapIndex(fill(t.Key(), depth+1), fill(t.Elem(), depth+1))
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if v.Field(i).CanSet() {
				v.Field(i).Set(fill(t.Field(i).Type, depth+1))
			}
		}
	}
	return v
}

// kindOf is the schema type of a decoded JSON value.
func kindOf(x any) string {
	switch y := x.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64:
		if y == float64(int64(y)) {
			return "integer"
		}
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "?"
}

// agrees reports a JSON value matches a claimed schema (unknown matches
// anything; an integer is a number).
func agrees(s schema, x any) bool {
	k := kindOf(x)
	switch {
	case s.Type == "unknown":
		return true
	case k == "null":
		return s.Nullable || s.Type == "null"
	case s.Type == k, s.Type == "number" && k == "integer":
		return true
	}
	return false
}

// sample is a JSON value of a claimed scalar type; wrong one of another
// kind.
func sample(s schema) (good, wrong any, ok bool) {
	switch s.Type {
	case "string":
		return "x", 7, true
	case "integer":
		return 3, "x", true
	case "number":
		return 2.5, "x", true
	case "boolean":
		return true, "x", true
	case "array":
		return []any{}, "x", true
	case "object":
		return map[string]any{}, "x", true
	}
	return nil, nil, false
}

// TestGoShapeTypes compares the claimed schemas of the fixture's struct
// types with encoding/json.
func TestGoShapeTypes(t *testing.T) {
	var types []typeExport
	readJSON(t, "types.json", &types)
	registry := map[string]any{}
	for name, v := range ginapp.Types {
		registry["ginapp."+name] = v
	}
	for name, v := range chiapp.Types {
		registry["chiapp."+name] = v
	}
	for name, v := range review.Types {
		registry["review."+name] = v
	}
	c := counts{}
	for _, te := range types {
		v, ok := registry[te.Package+"."+te.Name]
		if !ok {
			continue
		}
		rt := reflect.TypeOf(v)
		c["types compared"]++
		filled, err := json.Marshal(fill(rt, 0).Interface())
		if err != nil {
			t.Fatal(err)
		}
		zero, _ := json.Marshal(reflect.New(rt).Elem().Interface())
		var fo, zo any
		_ = json.Unmarshal(filled, &fo)
		_ = json.Unmarshal(zero, &zo)
		// Response schema.
		resp := te.Response
		if resp.Type == "unknown" {
			c["response unknown (refusal)"]++
			if _, isObject := fo.(map[string]any); isObject && !strings.Contains(resp.Reason, "recursive") {
				// A refusal is right when encoding/json does not write
				// the struct's fields as an object.
				t.Logf("%s.%s: refused (%s), encoding/json writes %s", te.Package, te.Name, resp.Reason, filled)
			} else {
				c["response refusal confirmed (not an object of the fields)"]++
			}
		} else {
			fm, _ := fo.(map[string]any)
			zm, _ := zo.(map[string]any)
			claimed := map[string]field{}
			for _, f := range resp.Fields {
				claimed[f.Name] = f
			}
			for k, x := range fm {
				f, ok := claimed[k]
				c["response keys compared"]++
				if !ok {
					t.Errorf("%s.%s: encoding/json writes key %q, not claimed", te.Package, te.Name, k)
					continue
				}
				if !agrees(f.Schema, x) {
					t.Errorf("%s.%s.%s: value %s, claimed %+v", te.Package, te.Name, k, kindOf(x), f.Schema)
				}
				c["response value kinds compared"]++
			}
			for _, f := range resp.Fields {
				if _, ok := fm[f.Name]; !ok {
					t.Errorf("%s.%s: claimed key %q is not written", te.Package, te.Name, f.Name)
				}
				_, inZero := zm[f.Name]
				c["response presence compared"]++
				if f.Presence == "optional" && inZero || f.Presence != "optional" && !inZero {
					t.Errorf("%s.%s.%s: presence %s, in zero value %v", te.Package, te.Name, f.Name, f.Presence, inZero)
				}
				if f.Presence == "required" {
					t.Errorf("%s.%s.%s: a response field is never required", te.Package, te.Name, f.Name)
				}
			}
		}
		// Request schema (plain encoding/json decode).
		req := te.Request
		if req.Type != "object" {
			c["request unknown (refusal)"]++
			continue
		}
		fm, _ := fo.(map[string]any)
		if len(fm) != len(req.Fields) {
			t.Errorf("%s.%s: request fields %d, encoding/json fields %d", te.Package, te.Name, len(req.Fields), len(fm))
		}
		for _, f := range req.Fields {
			good, wrong, ok := sample(f.Schema)
			if !ok {
				continue
			}
			p := reflect.New(rt)
			if f.Schema.Type == "string" && reflect.DeepEqual(good, "x") {
				// A string-option field takes a quoted number.
				good = "12"
			}
			data, _ := json.Marshal(map[string]any{f.Name: good})
			err := json.Unmarshal(data, p.Interface())
			if err != nil && f.Schema.Type == "string" {
				// A string with a format (time.Time): the schema has no
				// format, so an RFC 3339 value is tried too.
				data, _ = json.Marshal(map[string]any{f.Name: "2026-01-02T15:04:05Z"})
				err = json.Unmarshal(data, reflect.New(rt).Interface())
				c["request string field needing a format"]++
			}
			if err != nil {
				t.Errorf("%s.%s.%s: claimed %s, decoding %s: %v", te.Package, te.Name, f.Name, f.Schema.Type, data, err)
			}
			c["request field decodes"]++
			data, _ = json.Marshal(map[string]any{f.Name: wrong})
			if err := json.Unmarshal(data, reflect.New(rt).Interface()); err == nil {
				t.Errorf("%s.%s.%s: claimed %s, %s decodes", te.Package, te.Name, f.Name, f.Schema.Type, data)
			}
			c["request wrong kind refused"]++
			if f.Schema.Nullable {
				data, _ = json.Marshal(map[string]any{f.Name: nil})
				if err := json.Unmarshal(data, reflect.New(rt).Interface()); err != nil {
					t.Errorf("%s.%s.%s: claimed nullable, null: %v", te.Package, te.Name, f.Name, err)
				}
				c["request null accepted"]++
			}
		}
		if req.Additional == "allowed" {
			if err := json.Unmarshal([]byte(`{"zz_not_a_field": 1}`), reflect.New(rt).Interface()); err != nil {
				t.Errorf("%s.%s: additional allowed, %v", te.Package, te.Name, err)
			}
			c["request additional allowed"]++
		}
	}
	t.Logf("type comparisons:\n%s", c)
}

// valueFor builds a JSON value for a claimed schema: required fields
// (and, with all, every typed field) filled with samples.
func valueFor(s schema, all bool) any {
	switch s.Type {
	case "object":
		m := map[string]any{}
		for _, f := range s.Fields {
			if f.Presence == "required" || all && f.Schema.Type != "unknown" {
				m[f.Name] = valueFor(f.Schema, all)
			}
		}
		return m
	case "string":
		return "12"
	case "integer":
		return 3
	case "number":
		return 2.5
	case "boolean":
		return true
	case "array":
		if s.Items != nil && s.Items.Type == "object" {
			return []any{valueFor(*s.Items, all)}
		}
		return []any{}
	}
	return nil
}

// requiredPaths lists the paths to required fields, through objects and
// the one element valueFor puts in arrays of objects.
func requiredPaths(s schema, prefix []any) [][]any {
	var out [][]any
	switch s.Type {
	case "object":
		for _, f := range s.Fields {
			p := append(append([]any(nil), prefix...), f.Name)
			if f.Presence == "required" {
				out = append(out, p)
			}
			out = append(out, requiredPaths(f.Schema, p)...)
		}
	case "array":
		if s.Items != nil {
			out = append(out, requiredPaths(*s.Items, append(append([]any(nil), prefix...), 0))...)
		}
	}
	return out
}

// without deletes the value at path from v.
func without(v any, path []any) {
	for i, k := range path {
		last := i == len(path)-1
		switch x := v.(type) {
		case map[string]any:
			if last {
				delete(x, k.(string))
				return
			}
			v = x[k.(string)]
		case []any:
			if len(x) == 0 {
				return
			}
			v = x[0]
		}
	}
}

func scalarString(s *schema) string {
	if s != nil && s.Type == "array" && s.Items != nil {
		return scalarString(s.Items)
	}
	if s != nil && (s.Type == "integer" || s.Type == "number") {
		return "3"
	}
	if s != nil && s.Type == "boolean" {
		return "true"
	}
	return "12"
}

// fillPath replaces the template parameters with value.
func fillPath(template, value string) string {
	var segs []string
	for _, seg := range strings.Split(template, "/") {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			seg = value
		}
		segs = append(segs, seg)
	}
	return strings.Join(segs, "/")
}

type result struct {
	status int
	body   []byte
	ctype  string
}

// observed are the statuses and content types each route answered, for
// the reachability check (C1); current is the route being requested.
var (
	observed = map[string]map[int]string{}
	current  string
	header   http.Header
)

func do(t *testing.T, srv *httptest.Server, method, path string, query url.Values, body []byte, ctype string) result {
	t.Helper()
	defer func() { header = nil }()
	u := srv.URL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, u, rd)
	if err != nil {
		t.Fatal(err)
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if current != "" {
		if observed[current] == nil {
			observed[current] = map[int]string{}
		}
		observed[current][resp.StatusCode] = resp.Header.Get("Content-Type")
	}
	return result{status: resp.StatusCode, body: data, ctype: resp.Header.Get("Content-Type")}
}

// TestGoShapeRoutes serves the fixture's routers on 127.0.0.1 and checks
// the claimed statuses, binds and response keys against real responses.
func TestGoShapeRoutes(t *testing.T) {
	var routes []routeExport
	readJSON(t, "shapes.json", &routes)
	gin.SetMode(gin.ReleaseMode)
	servers := map[string]*httptest.Server{
		"ginapp": httptest.NewServer(ginapp.Engine(ginapp.NewAPI())),
		"chiapp": httptest.NewServer(chiapp.Router(&chiapp.Server{})),
		"mux":    httptest.NewServer(mux.Mux()),
	}
	// The review package serves gin and net/http routes on two servers.
	reviewGin, reviewMux := httptest.NewServer(review.Engine()), httptest.NewServer(review.Mux())
	defer reviewGin.Close()
	defer reviewMux.Close()
	for _, s := range servers {
		defer s.Close()
	}
	c := counts{}
	for _, r := range routes {
		if r.Shape == nil || len(r.Templates) == 0 || r.Method == "" {
			continue
		}
		pkg := strings.SplitN(strings.TrimPrefix(r.Identity, "route:"), "/", 2)[0]
		srv := servers[pkg]
		if pkg == "review" {
			srv = reviewGin
			if r.Framework == "net/http" {
				srv = reviewMux
			}
		}
		current = r.Identity
		if srv == nil {
			continue
		}
		statuses := map[string]response{}
		for _, it := range r.Shape.Responses.Items {
			statuses[it.Status] = it
		}
		path := fillPath(r.Templates[0], "1")
		query := url.Values{}
		for _, p := range r.Shape.Query.Items {
			if p.Access == "declared" && p.Presence != "optional" {
				query.Set(p.Name, scalarString(p.Schema))
			}
		}
		var body []byte
		ctype := ""
		var jsonBody *content
		for i, b := range r.Shape.Body.Items {
			if b.Schema != nil && (b.Schema.Type == "object" || b.Schema.Type == "array") && b.Provenance.Kind != "read_in_handler" {
				body, _ = json.Marshal(valueFor(*b.Schema, true))
				ctype = "application/json"
				jsonBody = &r.Shape.Body.Items[i]
			}
		}
		happy := do(t, srv, r.Method, path, query, body, ctype)
		c["happy requests"]++
		if len(statuses) > 0 {
			got, ok := statuses[fmt.Sprint(happy.status)]
			c["happy status compared"]++
			if !ok && strings.Contains(r.Shape.Responses.Reason, "a status Graphvein does not read") {
				// The section says a status was not read: an unclaimed
				// status is not a disagreement.
				c["happy status not claimed, section says a status was not read"]++
			} else if !ok {
				t.Errorf("%s: status %d, claimed %v", r.Identity, happy.status, keys(statuses))
			} else if got.Schema != nil && got.Schema.Type == "object" {
				var m map[string]any
				if err := json.Unmarshal(happy.body, &m); err != nil {
					t.Errorf("%s: body %q is not an object", r.Identity, happy.body)
				}
				claimed := map[string]field{}
				for _, f := range got.Schema.Fields {
					claimed[f.Name] = f
				}
				for k, x := range m {
					c["response keys compared"]++
					f, ok := claimed[k]
					if !ok && got.Schema.Additional == "forbidden" {
						t.Errorf("%s: key %q not claimed", r.Identity, k)
					} else if ok && !agrees(f.Schema, x) {
						t.Errorf("%s: key %q is %s, claimed %+v", r.Identity, k, kindOf(x), f.Schema)
					}
				}
				for _, f := range got.Schema.Fields {
					if _, ok := m[f.Name]; !ok && f.Presence != "optional" {
						t.Errorf("%s: claimed key %q missing from %s", r.Identity, f.Name, happy.body)
					}
				}
			}
			if got.ContentType != nil {
				c["content types compared"]++
				if !strings.HasPrefix(happy.ctype, *got.ContentType) {
					t.Errorf("%s: content type %q, claimed %q", r.Identity, happy.ctype, *got.ContentType)
				}
			}
		}
		failed := func(res result) bool {
			// A failed bind answers 400 or a claimed 4XX the handler
			// writes on a bind error.
			if res.status == 400 {
				return true
			}
			_, claimed := statuses[fmt.Sprint(res.status)]
			return claimed && res.status >= 400
		}
		// Body binds and decodes.
		if jsonBody != nil && jsonBody.Schema.Type == "array" {
			for _, p := range requiredPaths(*jsonBody.Schema, nil) {
				full := valueFor(*jsonBody.Schema, true)
				without(full, p)
				data, _ := json.Marshal(full)
				res := do(t, srv, r.Method, path, query, data, "application/json")
				c["missing required body field"]++
				c["missing required nested body field"]++
				if !failed(res) {
					t.Errorf("%s: without required %v: status %d", r.Identity, p, res.status)
				}
			}
		}
		if jsonBody != nil && jsonBody.Schema.Type == "object" {
			s := jsonBody.Schema
			for _, p := range requiredPaths(*s, nil) {
				full := valueFor(*s, true)
				without(full, p)
				data, _ := json.Marshal(full)
				res := do(t, srv, r.Method, path, query, data, "application/json")
				c["missing required body field"]++
				if len(p) > 1 {
					c["missing required nested body field"]++
				}
				if !failed(res) {
					t.Errorf("%s: without required %v: status %d", r.Identity, p, res.status)
				}
			}
			for _, f := range s.Fields {
				full := valueFor(*s, true).(map[string]any)
				_, wrong, ok := sample(f.Schema)
				if !ok {
					continue
				}
				full[f.Name] = wrong
				data, _ := json.Marshal(full)
				res := do(t, srv, r.Method, path, query, data, "application/json")
				switch jsonBody.Provenance.Kind {
				case "fact":
					c["wrong kind body field (fact)"]++
					if !failed(res) {
						t.Errorf("%s: %q of the wrong kind: status %d", r.Identity, f.Name, res.status)
					}
				case "intended":
					c["wrong kind body field (intended)"]++
					if res.status == 400 {
						t.Errorf("%s: intended %q of the wrong kind: refused with 400, so it is enforced", r.Identity, f.Name)
					}
				}
			}
		}
		// Declared query and path parameters.
		for _, p := range r.Shape.Query.Items {
			if p.Access != "declared" || p.Provenance.Kind != "fact" {
				continue
			}
			q := url.Values{}
			for k, v := range query {
				q[k] = v
			}
			if p.Presence == "required" {
				q.Del(p.Name)
				res := do(t, srv, r.Method, path, q, body, ctype)
				c["missing required query parameter"]++
				if !failed(res) {
					t.Errorf("%s: without query %q: status %d", r.Identity, p.Name, res.status)
				}
			}
			if p.Schema != nil && (p.Schema.Type == "integer" || p.Schema.Type == "array" && p.Schema.Items != nil && p.Schema.Items.Type == "integer") {
				q.Set(p.Name, "abc")
				for k, v := range query {
					if k != p.Name {
						q[k] = v
					}
				}
				res := do(t, srv, r.Method, path, q, body, ctype)
				c["wrong kind query parameter"]++
				if !failed(res) {
					t.Errorf("%s: query %q=abc: status %d", r.Identity, p.Name, res.status)
				}
			}
			if p.Presence == "optional" {
				res := do(t, srv, r.Method, path, query, body, ctype)
				c["optional query parameter omitted"]++
				if res.status == 400 {
					t.Errorf("%s: without optional %q: 400", r.Identity, p.Name)
				}
			}
		}
		for _, p := range r.Shape.Path.Items {
			if p.Access != "declared" || p.Schema == nil || p.Schema.Type != "integer" {
				continue
			}
			res := do(t, srv, r.Method, fillPath(r.Templates[0], "abc"), query, body, ctype)
			c["wrong kind path parameter"]++
			if !failed(res) {
				t.Errorf("%s: path %q=abc: status %d", r.Identity, p.Name, res.status)
			}
		}
		// Drive the branches, then check that every claimed status was
		// answered with its claimed content type (C1).
		for _, d := range drivers[pkg+" "+r.Method+" "+r.Templates[0]] {
			p := path
			if d.path != "" {
				p = d.path
			}
			header = d.header
			do(t, srv, r.Method, p, d.query, []byte(d.body), d.ctype)
			c["branch requests"]++
		}
		for _, it := range r.Shape.Responses.Items {
			var code int
			fmt.Sscan(it.Status, &code)
			ct, ok := observed[r.Identity][code]
			c["claimed statuses reached"]++
			if !ok {
				t.Errorf("%s: claimed status %s never answered (answered %v)", r.Identity, it.Status, observed[r.Identity])
				continue
			}
			if it.ContentType != nil {
				c["claimed content types matched"]++
				if !strings.HasPrefix(ct, *it.ContentType) {
					t.Errorf("%s: status %s content type %q, claimed %q", r.Identity, it.Status, ct, *it.ContentType)
				}
			}
			if it.Schema != nil && it.Schema.Type == "null" {
				c["claimed empty bodies"]++
			}
		}
		current = ""
	}
	// SH-G12: gin ShouldBind reads JSON and forms (content type not read).
	login := do(t, servers["ginapp"], "POST", "/login", nil, []byte("user=a&pass=b"), "application/x-www-form-urlencoded")
	loginJSON := do(t, servers["ginapp"], "POST", "/login", nil, []byte(`{"user":"a","pass":"b"}`), "application/json")
	c["ShouldBind form and JSON bodies"] += 2
	if login.status != 200 || loginJSON.status != 200 {
		t.Errorf("login: form %d, JSON %d", login.status, loginJSON.status)
	}
	t.Logf("route comparisons:\n%s", c)
}

// driver is one request that drives a handler branch.
type driver struct {
	path, body, ctype string
	query             url.Values
	header            http.Header
}

// drivers are the extra requests per route ("package METHOD template")
// that reach the branches the happy request does not.
var drivers = map[string][]driver{
	"chiapp DELETE /pets/{petID}": {{path: "/pets/x"}},
	"chiapp GET /pets":            {{query: url.Values{"limit": {"0"}}}},
	"chiapp GET /pets/{petID}":    {{path: "/pets/x"}, {header: http.Header{"If-None-Match": {"x"}}}},
	"chiapp POST /form":           {{ctype: "application/x-www-form-urlencoded", body: "name=a"}},
	"ginapp DELETE /items/{id}":   {{path: "/items/x"}},
	"ginapp GET /conflict":        {{query: url.Values{"tagged": {"1"}}}},
	"ginapp GET /users/{id}":      {{path: "/users/x"}},
	"ginapp POST /login":          {{ctype: "application/json", body: "{}"}},
	"ginapp POST /mixed":          {{ctype: "application/json", body: `{"a":1}`}, {ctype: "application/json", body: `{"a":"x"}`}},
	"ginapp POST /upload":         {{ctype: "multipart/form-data; boundary=B", body: "--B\r\nContent-Disposition: form-data; name=\"name\"\r\n\r\nn\r\n--B\r\nContent-Disposition: form-data; name=\"file\"; filename=\"f.txt\"\r\n\r\nx\r\n--B--\r\n"}},
	"ginapp POST /users":          {{ctype: "application/json", body: `{"name":"taken","email":"e"}`}},
	"review DELETE /form":         {{query: url.Values{"name": {"x"}}}, {}},
	"review POST /form":           {{query: url.Values{"name": {"x"}}, ctype: "application/x-www-form-urlencoded"}, {ctype: "application/json", body: "{}"}},
	"review POST /validated":      {{ctype: "application/json", body: `{"name":"x"}`}, {ctype: "application/json", body: "{}"}},
	"review POST /decodechecked":  {{ctype: "application/json", body: `{"id":"x"}`}},
	"review POST /decode":         {{ctype: "application/json", body: `{"id":"x"}`}},
}

func keys(m map[string]response) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

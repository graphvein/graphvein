package server

import (
	"net/http"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/store"
)

const modelTablesFixture = "../../testdata/fixtures/route-model-tables"

// modelTablesRoot indexes a copy of the route-model-tables fixture as
// "fixture".
func modelTablesRoot(t *testing.T, w store.Store) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(modelTablesFixture)); err != nil {
		t.Fatal(err)
	}
	indexRoot(t, root, w)
	return root
}

// tableRoutes is the part of a /schema/table response these tests read.
type tableRoutes struct {
	ModelRoutes *modelRoutesDoc `json:"model_routes"`
}

// routeUse is a compact form of one listed use: "METHOD path direction
// section status via nested row".
func routeUses(doc *modelRoutesDoc) []string {
	var out []string
	for _, r := range doc.Routes {
		for _, u := range r.Uses {
			status := ""
			if u.Status != nil {
				status = *u.Status
			}
			out = append(out, strings.Join([]string{*r.Method, *r.Path, r.Model, u.Direction, u.Section, status, strings.Join(u.Via, ">"), boolText(u.Nested), u.Row, u.Provenance.Rule}, " "))
		}
	}
	return out
}

func boolText(b bool) string {
	if b {
		return "nested"
	}
	return "direct"
}

// TestModelRoutesFixture checks rows MT-21, MT-23, MT-24 and MT-25 on the
// route-model-tables fixture: the Hero table lists the routes that take
// the Hero table class as the body or return it (also as a list), never
// the route returning HeroCopy (the same fields) or the other file's Hero
// (the same name); the Team table lists the nested use through TeamList
// and the body and nullable response of PUT; Audit lists none.
func TestModelRoutesFixture(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := modelTablesRoot(t, w)
		opt := testOptions(root, s)
		table := func(id string) *modelRoutesDoc {
			t.Helper()
			var body tableRoutes
			decodeInto(t, get(t, opt, http.MethodGet, schemaPath("table", id), nil), &body)
			if body.ModelRoutes == nil {
				t.Fatalf("%s: no model_routes", id)
			}
			return body.ModelRoutes
		}
		hero := table("table:app/models.py#Hero")
		if hero.State != modelRoutesListed || hero.Row != rowRoutesListed || hero.Class == nil || *hero.Class != "class:app/models.py#Hero" {
			t.Fatalf("hero %+v", hero)
		}
		want := []string{
			"GET /heroes Hero response responses 200  direct MT-23 SH-F26",
			"POST /heroes Hero request body   direct MT-21 SH-F18",
			"GET /heroes/{hero_id} Hero response responses 200  direct MT-23 SH-F26",
		}
		if got := routeUses(hero); !reflect.DeepEqual(got, want) {
			t.Errorf("hero uses\n got %q\nwant %q", got, want)
		}
		if hero.RoutesTotal != 7 || hero.RoutesWithShape != 7 || hero.RoutesNotFullyRead != 7 {
			t.Errorf("hero counts %d %d %d", hero.RoutesTotal, hero.RoutesWithShape, hero.RoutesNotFullyRead)
		}
		for _, r := range hero.Routes {
			for _, u := range r.Uses {
				if u.Provenance.Kind != routeshape.Fact || u.Provenance.File != "app/main.py" {
					t.Errorf("%s provenance %+v", r.Route, u.Provenance)
				}
				if hero.Links[shapeLinkKey(u.Provenance.File, u.Provenance.Line)] == nil {
					t.Errorf("%s: no editor link for its chip", r.Route)
				}
			}
		}
		team := table("table:app/models.py#Team")
		want = []string{
			"GET /teams Team response responses 200 TeamList nested MT-24 SH-F26",
			"PUT /teams/{team_id} Team request body   direct MT-21 SH-F18",
			"PUT /teams/{team_id} Team response responses 200  direct MT-23 SH-F26",
		}
		if got := routeUses(team); team.State != modelRoutesListed || !reflect.DeepEqual(got, want) {
			t.Errorf("team %s uses\n got %q\nwant %q", team.State, got, want)
		}
		audit := table("table:app/models.py#Audit")
		if audit.State != modelRoutesNoneFound || audit.Row != rowRoutesAbsence || len(audit.Routes) != 0 || audit.RoutesNotFullyRead != 7 {
			t.Errorf("audit %+v", audit)
		}
	})
}

// TestRouteModelTablesFixture checks row MT-28: the route detail names
// the shape's models that are also tables, and only those.
func TestRouteModelTablesFixture(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := modelTablesRoot(t, w)
		opt := testOptions(root, s)
		detail := func(method, path string) *[]routeModelTable {
			t.Helper()
			var body struct {
				ModelTables *[]routeModelTable `json:"model_tables"`
			}
			rec := get(t, opt, http.MethodGet, "/api/v0/api/endpoint?scope=all&id="+url.QueryEscape(contractRouteID(method, path)), nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body.String())
			}
			decodeInto(t, rec, &body)
			return body.ModelTables
		}
		name, set, tool := "hero", "models/sqlmodel/.", "sqlmodel"
		got := detail("POST", "/heroes")
		want := []routeModelTable{{Model: "Hero", Class: "class:app/models.py#Hero", Table: "table:app/models.py#Hero", Name: &name, Set: &set, Tool: &tool, Row: rowRouteModelTables}}
		if got == nil || !reflect.DeepEqual(*got, want) {
			t.Errorf("POST /heroes model_tables %+v", got)
		}
		for _, p := range [][2]string{{"GET", "/copies/{copy_id}"}, {"GET", "/others/{other_id}"}} {
			if got := detail(p[0], p[1]); got == nil || len(*got) != 0 {
				t.Errorf("%s %s model_tables %+v, want []", p[0], p[1], got)
			}
		}
		if got := detail("GET", "/teams"); got == nil || len(*got) != 1 || (*got)[0].Table != "table:app/models.py#Team" {
			t.Errorf("GET /teams model_tables %+v", got)
		}
	})
}

// contractRouteID is the identity the FastAPI extractor gives a route of
// app/main.py.
func contractRouteID(method, path string) string {
	return "route:app/main.py#" + strings.NewReplacer(" ", "%20", "/", "%2F", "{", "%7B", "}", "%7D").Replace(method+" "+path)
}

// classNode is a stored class node at file:line.
func classNode(file, name string, line int) contract.Node {
	return contract.Node{Identity: "class:" + file + "#" + name, Kind: "class", Name: name, File: file, Span: &contract.Span{StartLine: line, EndLine: line + 2}}
}

// TestModelTableIdentity checks row MT-20 (the model is the class node:
// same file, same line, the class's name or SH-F22's qualified form) and
// row MT-29 (everything else is never linked: another line, file or name,
// a model mapped from a spec, a node that is not a class).
func TestModelTableIdentity(t *testing.T) {
	class := classNode("app/models.py", "Hero", 9)
	model := func(name, file string, line int, kind routeshape.Kind) routeshape.Model {
		return routeshape.Model{Name: name, Provenance: routeshape.Provenance{Kind: kind, File: file, Line: line, Rule: "SH-F22", Confidence: 1}}
	}
	for _, tc := range []struct {
		name string
		m    routeshape.Model
		c    contract.Node
		want bool
	}{
		{"same declaration", model("Hero", "app/models.py", 9, routeshape.Fact), class, true},
		{"qualified name", model("Hero__app_models", "app/models.py", 9, routeshape.Fact), class, true},
		{"intended", model("Hero", "app/models.py", 9, routeshape.Intended), class, true},
		{"returned", model("Hero", "app/models.py", 9, routeshape.Returned), class, true},
		{"mapped from a spec", model("Hero", "app/models.py", 9, routeshape.Mapped), class, false},
		{"read in handler", model("Hero", "app/models.py", 9, routeshape.ReadInHandler), class, false},
		{"another line", model("Hero", "app/models.py", 10, routeshape.Fact), class, false},
		{"another file", model("Hero", "app/other.py", 9, routeshape.Fact), class, false},
		{"another name", model("HeroCopy", "app/models.py", 9, routeshape.Fact), class, false},
		{"name prefix only", model("Heroes", "app/models.py", 9, routeshape.Fact), class, false},
		{"not a class node", model("Hero", "app/models.py", 9, routeshape.Fact), contract.Node{Kind: "interface", Name: "Hero", File: "app/models.py", Span: &contract.Span{StartLine: 9}}, false},
		{"class without span", model("Hero", "app/models.py", 9, routeshape.Fact), contract.Node{Kind: "class", Name: "Hero", File: "app/models.py"}, false},
	} {
		if got := modelIsClass(tc.m, tc.c); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	table := func(class any) contract.Node {
		return contract.Node{Kind: "table", Attributes: map[string]any{"source": "model", "class": class}}
	}
	if tableClass(table("class:app/models.py#Hero")) != "class:app/models.py#Hero" {
		t.Error("a class node identity is the declaration")
	}
	for _, v := range []any{"User", "table:app/models.py#Hero", nil, 3} {
		if tableClass(table(v)) != "" {
			t.Errorf("class attribute %v is not a class node identity", v)
		}
	}
}

// synthetic builds a shape whose every section is read (complete or
// provably none) with a fact provenance.
func synthetic() *routeshape.Shape {
	s := routeshape.New(6)
	fact := routeshape.Provenance{Kind: routeshape.Fact, Rule: "SH-T1", File: "app.py", Line: 3, Confidence: 1}
	for _, sec := range []*routeshape.Section{&s.Path.Section, &s.Query.Section, &s.Header.Section, &s.Cookie.Section, &s.Body.Section, &s.Responses.Section} {
		*sec = routeshape.Section{State: routeshape.ProvablyNone, Evidence: &fact}
	}
	return s
}

// TestModelUsesSynthetic checks rows MT-22 (a parameter whose schema is
// the model) and MT-24 (nested uses through fields and models, a cycle
// ends), and that a security requirement is no use.
func TestModelUsesSynthetic(t *testing.T) {
	fact := routeshape.Provenance{Kind: routeshape.Fact, Rule: "SH-T1", File: "app.py", Line: 3, Confidence: 1}
	s := synthetic()
	ref := func(name string) *routeshape.Schema { return &routeshape.Schema{Type: routeshape.TypeRef, Ref: name} }
	s.Query = routeshape.ParamSection{Section: routeshape.Section{State: routeshape.Complete, Evidence: &fact}, Items: []routeshape.Param{
		{Name: "filter", Access: routeshape.Declared, Presence: routeshape.Required, Schema: ref("Row"), Provenance: fact},
	}}
	s.Models = []routeshape.Model{
		{Name: "Row", Schema: routeshape.Schema{Type: routeshape.TypeObject, Additional: routeshape.AdditionalForbidden}, Provenance: fact},
		{Name: "Page", Provenance: fact, Schema: routeshape.Schema{Type: routeshape.TypeObject, Additional: routeshape.AdditionalForbidden, Fields: []routeshape.Field{
			{Name: "rows", Presence: routeshape.Required, Schema: routeshape.Schema{Type: routeshape.TypeArray, Items: ref("Row")}},
			{Name: "next", Presence: routeshape.Optional, Schema: *ref("Page")},
		}}},
	}
	json := "application/json"
	s.Responses = routeshape.ResponseSection{Section: routeshape.Section{State: routeshape.Complete, Evidence: &fact}, Items: []routeshape.Response{
		{Status: "200", ContentType: &json, Schema: ref("Page"), Provenance: fact},
		{Status: "201", ContentType: &json, Schema: &routeshape.Schema{Type: routeshape.TypeArray, Items: &routeshape.Schema{Type: routeshape.TypeArray, Items: ref("Row")}}, Provenance: fact},
	}}
	uses := modelUses(s, "Row")
	if len(uses) != 3 {
		t.Fatalf("uses %+v", uses)
	}
	if u := uses[0]; u.Direction != "request" || u.Section != "query" || u.Name == nil || *u.Name != "filter" || u.Nested || u.Row != rowUseParameter {
		t.Errorf("parameter use %+v", u)
	}
	if u := uses[1]; u.Direction != "response" || *u.Status != "200" || !u.Nested || !reflect.DeepEqual(u.Via, []string{"Page"}) || u.Row != rowUseNested {
		t.Errorf("nested use %+v", u)
	}
	if u := uses[2]; *u.Status != "201" || u.Nested || len(u.Via) != 0 || u.Row != rowUseResponse {
		t.Errorf("array use %+v", u)
	}
	if !shapeFullyRead(s) {
		t.Error("every section and schema is read")
	}
	if got := modelUses(s, "Missing"); len(got) != 0 {
		t.Errorf("no use of an absent model: %+v", got)
	}
}

// routeWith is a stored route node holding a shape (nil: none).
func routeWith(t *testing.T, path string, s *routeshape.Shape) contract.Node {
	t.Helper()
	attrs := map[string]any{"method": "GET", "pattern": path, "paths": []any{map[string]any{"template": path}}}
	if s != nil {
		v, err := routeshape.ToAttribute(s)
		if err != nil {
			t.Fatal(err)
		}
		attrs[routeshape.AttributeKey] = v
	}
	return contract.Node{Identity: "route:app.py#GET%20" + url.PathEscape(path), Kind: "route", File: "app.py", Attributes: attrs}
}

// TestModelRoutesAbsence checks row MT-26: "none" only when every route
// stores a shape that is read to its end; "none_found" when a route has
// no shape, a section not complete, or an unknown schema; "not_read" when
// no route stores a shape (or there are no routes).
func TestModelRoutesAbsence(t *testing.T) {
	class := classNode("app/models.py", "Audit", 35)
	read := synthetic()
	if !shapeFullyRead(read) {
		t.Fatal("the synthetic shape is fully read")
	}
	partial := synthetic()
	partial.Responses.Section = routeshape.Section{State: routeshape.Partial, Reason: "error statuses not read"}
	fact := routeshape.Provenance{Kind: routeshape.Fact, Rule: "SH-T1", File: "app.py", Line: 3, Confidence: 1}
	unknown := synthetic()
	unknown.Body = routeshape.BodySection{Section: routeshape.Section{State: routeshape.Complete, Evidence: &fact}, Items: []routeshape.Content{
		{Schema: &routeshape.Schema{Type: routeshape.TypeUnknown, Reason: "not read"}, Provenance: fact},
	}}
	mapOf := synthetic()
	mapOf.Responses = routeshape.ResponseSection{Section: routeshape.Section{State: routeshape.Complete, Evidence: &fact}, Items: []routeshape.Response{
		// dict[str, Hero]: the value schema is not stored.
		{Status: "200", Schema: &routeshape.Schema{Type: routeshape.TypeObject, Additional: routeshape.AdditionalAllowed}, Provenance: fact},
	}}
	openModel := synthetic()
	openModel.Models = []routeshape.Model{{Name: "Wrapper", Provenance: fact, Schema: routeshape.Schema{Type: routeshape.TypeObject, Additional: routeshape.AdditionalUnknown}}}
	openModel.Responses = routeshape.ResponseSection{Section: routeshape.Section{State: routeshape.Complete, Evidence: &fact}, Items: []routeshape.Response{
		{Status: "200", Schema: &routeshape.Schema{Type: routeshape.TypeRef, Ref: "Wrapper"}, Provenance: fact},
	}}
	unreadSchema := synthetic()
	unreadSchema.Body = routeshape.BodySection{Section: routeshape.Section{State: routeshape.Complete, Evidence: &fact}, Items: []routeshape.Content{{Provenance: fact}}}
	for _, tc := range []struct {
		name   string
		routes []contract.Node
		state  string
		unread int
	}{
		{"every route read", []contract.Node{routeWith(t, "/a", read), routeWith(t, "/b", read)}, modelRoutesNone, 0},
		{"a route without shape", []contract.Node{routeWith(t, "/a", read), routeWith(t, "/b", nil)}, modelRoutesNoneFound, 1},
		{"a partial section", []contract.Node{routeWith(t, "/a", read), routeWith(t, "/b", partial)}, modelRoutesNoneFound, 1},
		{"an unknown schema", []contract.Node{routeWith(t, "/a", unknown)}, modelRoutesNoneFound, 1},
		{"a schema not read", []contract.Node{routeWith(t, "/a", unreadSchema)}, modelRoutesNoneFound, 1},
		{"a map (dict[str, Hero])", []contract.Node{routeWith(t, "/a", mapOf)}, modelRoutesNoneFound, 1},
		{"a model accepting other fields", []contract.Node{routeWith(t, "/a", openModel)}, modelRoutesNoneFound, 1},
		{"no shape stored", []contract.Node{routeWith(t, "/a", nil)}, modelRoutesNotRead, 1},
		{"no routes", nil, modelRoutesNotRead, 0},
	} {
		got := modelRoutesOf(nil, &class, tc.routes)
		if got.State != tc.state || got.Row != rowRoutesAbsence || got.RoutesNotFullyRead != tc.unread || len(got.Routes) != 0 {
			t.Errorf("%s: %+v", tc.name, got)
		}
	}
	// A spec-only section is not read by the code.
	spec := synthetic()
	spec.Query.Section = routeshape.Section{State: routeshape.Partial, Reason: routeshape.SpecOnlyReason}
	if shapeFullyRead(spec) {
		t.Error("a section mapped from a spec only is not read by the code")
	}
}

// TestModelRoutesNoDeclaration checks row MT-27: a model table that names
// no stored class node has no route list, and a migration table has no
// model_routes field.
func TestModelRoutesNoDeclaration(t *testing.T) {
	got := modelRoutesOf(nil, nil, []contract.Node{routeWith(t, "/a", synthetic())})
	if got.State != modelRoutesNoDeclaration || got.Row != rowNoDeclaration || got.Class != nil || len(got.Routes) != 0 {
		t.Errorf("no declaration %+v", got)
	}
	backends(t, func(t *testing.T, w, s store.Store) {
		root := modelTablesRoot(t, w)
		var body map[string]any
		decodeInto(t, get(t, testOptions(root, s), http.MethodGet, schemaPath("table", "table:app/models.py#Hero"), nil), &body)
		if _, ok := body["model_routes"]; !ok {
			t.Error("a model table has model_routes")
		}
		migrations := schemaFixtureRoot(t)
		indexRoot(t, migrations, w)
		body = nil
		decodeInto(t, get(t, testOptions(migrations, s), http.MethodGet, schemaPath("table", "table:pg-app/migrations/2_relations.sql#tasks"), nil), &body)
		if _, ok := body["model_routes"]; ok {
			t.Error("a migration table has model_routes")
		}
	})
}

// TestModelTableContractTable keeps the MT-20..MT-29 section of
// docs/contract/claims/route-model-types.md and modelTableRows 1:1, and
// checks that every row names an existing test.
func TestModelTableContractTable(t *testing.T) {
	doc, err := os.ReadFile("../../docs/contract/claims/route-model-types.md")
	if err != nil {
		t.Fatal(err)
	}
	var documented, code []string
	for _, m := range regexp.MustCompile(`(?m)^\| (MT-2[0-9]) \| ([^|]+) \|`).FindAllSubmatch(doc, -1) {
		documented = append(documented, string(m[1])+" "+strings.TrimSpace(string(m[2])))
	}
	for _, r := range modelTableRows {
		code = append(code, r.ID+" "+r.Topic)
	}
	if strings.Join(documented, ",") != strings.Join(code, ",") {
		t.Fatalf("route-model-types.md rows %v\ncode rows %v", documented, code)
	}
	sources, err := os.ReadFile("modeltables_test.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range modelTableRows {
		if !strings.Contains(string(sources), "func "+r.Test+"(t *testing.T)") {
			t.Errorf("row %s names test %s, which does not exist", r.ID, r.Test)
		}
	}
}

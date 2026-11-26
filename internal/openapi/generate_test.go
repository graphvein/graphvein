package openapi

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
	"go.yaml.in/yaml/v3"
)

// node builds a route node; attrs must hold method and framework.
func node(id, file string, line int, attrs map[string]any) contract.Node {
	return contract.Node{Type: "node", Identity: id, Kind: "route", Level: 4, File: file, ContentHash: contract.ContentHash([]byte(id)),
		Span: &contract.Span{StartLine: line, StartColumn: 0, EndLine: line, EndColumn: 30}, Attributes: attrs}
}

func entry(template, resolution string, extra ...any) map[string]any {
	e := map[string]any{"template": template, "conditional": false, "resolution": resolution}
	for i := 0; i+1 < len(extra); i += 2 {
		e[extra[i].(string)] = extra[i+1]
	}
	return e
}

func paths(entries ...map[string]any) []any {
	out := []any{}
	for _, e := range entries {
		out = append(out, e)
	}
	return out
}

func attribute(t *testing.T, s *routeshape.Shape) map[string]any {
	t.Helper()
	attr, err := routeshape.ToAttribute(s)
	if err != nil {
		t.Fatal(err)
	}
	return attr
}

// smGoldenShape is the shape served by the SM golden
// (internal/server/testdata/route-shape.golden.json).
func smGoldenShape(t *testing.T) any {
	t.Helper()
	data, err := os.ReadFile("../server/testdata/route-shape.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func str(s string) *string { return &s }

// hash is a content hash for a synthetic registration.
func hash(seed string) string { return contract.ContentHash([]byte(seed)) }

// hashed gives a synthetic route its registration hash.
func hashed(n contract.Node, seed string) contract.Node {
	n.ContentHash = hash(seed)
	return n
}

// syntheticInput holds one route per row family: shapes of every kind and
// state, every refusal, conflict, user value and auth form.
func syntheticInput(t *testing.T) Input {
	fact := routeshape.Provenance{Kind: routeshape.Fact, Rule: "SH-T1", File: "app/items.py", Line: 10, Confidence: 1}
	read := routeshape.Provenance{Kind: routeshape.ReadInHandler, Rule: "SH-T2", File: "app/items.py", Line: 12, Confidence: 1}
	intended := routeshape.Provenance{Kind: routeshape.Intended, Rule: "SH-T3", File: "app/items.py", Line: 13, Confidence: 1}
	returned := routeshape.Provenance{Kind: routeshape.Returned, Rule: "SH-T4", File: "app/items.py", Line: 14, Confidence: 1}
	mapped := routeshape.Provenance{Kind: routeshape.Mapped, Rule: "SH-T5", File: "openapi.yaml", Line: 20, Confidence: 0.9}
	appJSON := "application/json"

	// A second shape: every parameter form, bodies, responses, a model
	// whose name collides with the SM golden's Item but differs.
	s2 := routeshape.New(3)
	s2.Query = routeshape.ParamSection{Section: routeshape.Section{State: routeshape.Partial, Reason: "a callee was not resolved"}, Items: []routeshape.Param{
		{Name: "q", Access: routeshape.Declared, Presence: routeshape.Required, Schema: &routeshape.Schema{Type: routeshape.TypeString}, Provenance: fact},
		{Name: "limit", Access: routeshape.Declared, Presence: routeshape.Optional, Schema: &routeshape.Schema{Type: routeshape.TypeInteger, Nullable: true}, Provenance: fact},
		{Name: "maybe", Access: routeshape.Declared, Presence: routeshape.PresenceUnknown, Provenance: fact},
		{Name: "page", Access: routeshape.ReadByHandler, Presence: routeshape.PresenceUnknown, Provenance: read},
	}}
	s2.Header = routeshape.ParamSection{Section: routeshape.Section{State: routeshape.Partial, Reason: "middleware not read"}, Items: []routeshape.Param{
		{Name: "Authorization", Access: routeshape.Declared, Presence: routeshape.Required, Schema: &routeshape.Schema{Type: routeshape.TypeString}, Provenance: fact},
		{Name: "X-Trace", Access: routeshape.Declared, Presence: routeshape.Optional, Provenance: fact},
	}}
	s2.Cookie = routeshape.ParamSection{Section: routeshape.Section{State: routeshape.ProvablyNone, Evidence: &fact}}
	s2.Path = routeshape.ParamSection{Section: routeshape.Section{State: routeshape.Partial, Reason: "x"}, Items: []routeshape.Param{
		{Name: "ghost", Access: routeshape.Declared, Presence: routeshape.Required, Schema: &routeshape.Schema{Type: routeshape.TypeString}, Provenance: fact},
	}}
	s2.Models = []routeshape.Model{
		{Name: "Item", Provenance: fact, Schema: routeshape.Schema{Type: routeshape.TypeObject, Additional: routeshape.AdditionalUnknown, Fields: []routeshape.Field{
			{Name: "name", Presence: routeshape.Required, Schema: routeshape.Schema{Type: routeshape.TypeString}},
			{Name: "note", Presence: routeshape.PresenceUnknown, Schema: routeshape.Schema{Type: routeshape.TypeUnknown, Reason: "a custom validator"}},
			{Name: "parent", Presence: routeshape.Optional, Schema: routeshape.Schema{Type: routeshape.TypeRef, Ref: "Item", Nullable: true}},
		}}},
		{Name: "Draft[T]", Provenance: intended, Schema: routeshape.Schema{Type: routeshape.TypeObject, Additional: routeshape.AdditionalAllowed}},
	}
	s2.Body = routeshape.BodySection{Section: routeshape.Section{State: routeshape.Partial, Reason: "form fields not read"}, Items: []routeshape.Content{
		{ContentType: &appJSON, Schema: &routeshape.Schema{Type: routeshape.TypeRef, Ref: "Item"}, Provenance: fact},
		{ContentType: &appJSON, Schema: &routeshape.Schema{Type: routeshape.TypeRef, Ref: "Draft[T]"}, Provenance: intended},
		{ContentType: str("text/plain"), Provenance: fact},
		{Schema: &routeshape.Schema{Type: routeshape.TypeString}, Provenance: fact},
	}}
	s2.Responses = routeshape.ResponseSection{Section: routeshape.Section{State: routeshape.Partial, Reason: "error statuses not read"}, Items: []routeshape.Response{
		{Status: "201", ContentType: &appJSON, Schema: &routeshape.Schema{Type: routeshape.TypeArray, Items: &routeshape.Schema{Type: routeshape.TypeEnum, Enum: []any{"a", 1.0, true}, Nullable: true}}, Provenance: fact},
		{Status: "201", ContentType: &appJSON, Schema: &routeshape.Schema{Type: routeshape.TypeRef, Ref: "Item"}, Provenance: returned},
		{Status: "204", Schema: &routeshape.Schema{Type: routeshape.TypeNull}, Provenance: fact},
		{Status: "4XX", Schema: &routeshape.Schema{Type: routeshape.TypeBoolean}, Provenance: fact},
		{Status: "default", ContentType: &appJSON, Provenance: fact},
		{Status: "404", Provenance: mapped},
	}}
	s2.Security = routeshape.SecuritySection{Section: routeshape.Section{State: routeshape.Partial, Reason: "a dependency was not read"}, Items: []routeshape.Requirement{
		{Schemes: []routeshape.SchemeUse{{Name: "HTTPBearer"}}, Provenance: fact},
	}}
	bearer := "bearer"
	s2.Schemes = []routeshape.Scheme{{Name: "HTTPBearer", Type: routeshape.SchemeHTTP, Scheme: &bearer, Provenance: fact}}
	s2.Follow.Chains = []routeshape.ChainRecord{
		{Section: routeshape.SectionQuery, Start: routeshape.Step{File: "app/items.py", Line: 9, Function: "create"}, Steps: []routeshape.Step{}, Depth: 0, Stop: routeshape.StopUnresolved,
			At: &routeshape.Step{File: "app/items.py", Line: 11, Function: "helper"}},
		{Section: routeshape.SectionBody, Start: routeshape.Step{File: "app/items.py", Line: 9, Function: "create"}, Steps: []routeshape.Step{}, Depth: 0, Stop: routeshape.StopEnded},
	}

	// Security from the shape: an API key and the anonymous alternative
	// (written); OAuth2 (refused); provably none (never security: []).
	key := func(state routeshape.State, schemes []routeshape.Scheme, items ...routeshape.Requirement) *routeshape.Shape {
		s := routeshape.New(6)
		s.Security = routeshape.SecuritySection{Section: routeshape.Section{State: state}, Items: items}
		if state == routeshape.Complete || state == routeshape.ProvablyNone {
			s.Security.Evidence = &fact
		}
		s.Schemes = schemes
		return s
	}
	header, keyName := "header", "X-API-Key"
	apiKey := key(routeshape.Complete, []routeshape.Scheme{{Name: "ApiKey", Type: routeshape.SchemeAPIKey, In: &header, ParamName: &keyName, Provenance: fact}},
		routeshape.Requirement{Schemes: []routeshape.SchemeUse{{Name: "ApiKey"}}, Provenance: fact}, routeshape.Requirement{Schemes: []routeshape.SchemeUse{}, Provenance: fact})
	oauth := key(routeshape.Complete, []routeshape.Scheme{{Name: "OAuth2PasswordBearer", Type: routeshape.SchemeOAuth2, TokenURL: str("/token"), Provenance: fact}},
		routeshape.Requirement{Schemes: []routeshape.SchemeUse{{Name: "OAuth2PasswordBearer", Scopes: []string{"read"}}}, Provenance: fact})
	none := key(routeshape.ProvablyNone, nil)
	capped := routeshape.New(2)
	capped.Body.Section = routeshape.Section{State: routeshape.Partial, Reason: "depth cap reached (2)"}
	capped.Follow.Chains = []routeshape.ChainRecord{{Section: routeshape.SectionBody, Start: routeshape.Step{File: "src/app.ts", Line: 3, Function: "handler"},
		Steps: []routeshape.Step{{File: "src/app.ts", Line: 4, Function: "a"}, {File: "src/a.ts", Line: 2, Function: "b"}}, Depth: 2, Stop: routeshape.StopDepthCap,
		At: &routeshape.Step{File: "src/b.ts", Line: 2, Function: "c"}, Detail: "depth cap reached (2)"}}

	httpDep := func(class, name string, auto any) []any {
		return []any{map[string]any{"class": class, "scheme_name": name, "auto_error": auto, "instance": "app/security.py#x", "chain": []any{}}}
	}
	fastapi := func(method, pattern string, extra map[string]any) map[string]any {
		a := map[string]any{"framework": "fastapi", "method": method, "pattern": pattern, "handler": "function:app/items.py#h", "handler_inline": false, "middleware": []any{}}
		for k, v := range extra {
			a[k] = v
		}
		return a
	}
	express := func(method, pattern string, extra map[string]any) map[string]any {
		a := map[string]any{"framework": "express", "method": method, "pattern": pattern, "handler": nil, "handler_inline": true, "middleware": []any{"auth"}}
		for k, v := range extra {
			a[k] = v
		}
		return a
	}
	next := func(project string, entries []any) map[string]any {
		return map[string]any{"framework": "nextjs", "method": "GET", "pattern": "/api/x", "handler": "function:web/app/api/x/route.ts#GET", "handler_inline": false,
			"middleware": []any{}, "project": project, "base_path": nil, "paths": entries}
	}
	group := []any{"route:src/router.js#GET%20%2Fsame", "route:src/router.js#GET%20%2Fsame%402"}
	routes := []contract.Node{
		node("route:app/items.py#GET%20%2F%7Bid%7D", "app/items.py", 3, fastapi("GET", "/{id}", map[string]any{
			"paths":                 paths(entry("/api/items/{id}", "settings_default")),
			"security_dependencies": httpDep("HTTPBearer", "HTTPBearer", true), "security_dependencies_unknown": []any{},
			routeshape.AttributeKey: smGoldenShape(t)})),
		node("route:app/items.py#POST%20%2F", "app/items.py", 9, fastapi("POST", "/", map[string]any{
			"paths": paths(map[string]any{"template": "/api/items", "conditional": true, "resolution": "literal", "trailing_slash": true}), routeshape.AttributeKey: attribute(t, s2)})),
		node("route:app/items.py#GET%20%2Ffiles%2F%7Bp%3Apath%7D", "app/items.py", 20, fastapi("GET", "/files/{p:path}", map[string]any{"paths": paths(entry("/files/{p}", "constant"))})),
		node("route:app/auth.py#GET%20%2Fbearer", "app/auth.py", 3, fastapi("GET", "/bearer", map[string]any{"paths": paths(entry("/bearer", "literal")),
			"security_dependencies": httpDep("HTTPBasic", "Shared", true)})),
		node("route:app/auth.py#GET%20%2Fbasic", "app/auth.py", 4, fastapi("GET", "/basic", map[string]any{"paths": paths(entry("/basic", "literal")),
			"security_dependencies": httpDep("HTTPBearer", "Shared", true)})),
		node("route:app/auth.py#GET%20%2Foauth", "app/auth.py", 5, fastapi("GET", "/oauth", map[string]any{"paths": paths(entry("/oauth", "literal")),
			"security_dependencies": httpDep("OAuth2PasswordBearer", "OAuth2PasswordBearer", true)})),
		node("route:app/auth.py#GET%20%2Foptional", "app/auth.py", 6, fastapi("GET", "/optional", map[string]any{"paths": paths(entry("/optional", "literal")),
			"security_dependencies": httpDep("HTTPDigest", "HTTPDigest", false)})),
		node("route:app/auth.py#GET%20%2Fdigest", "app/auth.py", 7, fastapi("GET", "/digest", map[string]any{"paths": paths(entry("/digest", "literal")),
			"security_dependencies": httpDep("HTTPDigest", "HTTPDigest", true)})),
		node("route:app/auth.py#GET%20%2Fsame-scheme", "app/auth.py", 9, fastapi("GET", "/same-scheme", map[string]any{"paths": paths(entry("/same-scheme", "literal")),
			"security_dependencies": httpDep("HTTPBearer", "HTTPBearer", true)})),
		node("route:app/auth.py#GET%20%2Fpartial", "app/auth.py", 8, fastapi("GET", "/partial", map[string]any{"paths": paths(entry("/partial", "literal")),
			"security_dependencies": httpDep("HTTPDigest", "HTTPDigest", true), "security_dependencies_unknown": []any{map[string]any{"reason": "not read", "expression": "x", "chain": []any{}}}})),
		node("route:app/keys.py#GET%20%2Fkey", "app/keys.py", 2, fastapi("GET", "/key", map[string]any{"paths": paths(entry("/key", "literal")), routeshape.AttributeKey: attribute(t, apiKey)})),
		node("route:app/keys.py#GET%20%2Fflows", "app/keys.py", 3, fastapi("GET", "/flows", map[string]any{"paths": paths(entry("/flows", "literal")), routeshape.AttributeKey: attribute(t, oauth)})),
		node("route:app/keys.py#GET%20%2Fpublic", "app/keys.py", 4, fastapi("GET", "/public", map[string]any{"paths": paths(entry("/public", "literal")), routeshape.AttributeKey: attribute(t, none)})),
		node("route:app/keys.py#GET%20%2Fbroken", "app/keys.py", 5, fastapi("GET", "/broken", map[string]any{"paths": paths(entry("/broken", "literal")), routeshape.AttributeKey: map[string]any{"version": 2}})),
		node("route:src/app.ts#POST%20%2Fcapped", "src/app.ts", 3, express("POST", "/capped", map[string]any{"paths": paths(entry("/capped", "literal")), routeshape.AttributeKey: attribute(t, capped)})),
		node(group[0].(string), "src/router.js", 3, express("GET", "/same", map[string]any{"paths": paths(entry("/same", "literal")), "equivalent_registrations": group})),
		node(group[1].(string), "src/router.js", 4, express("GET", "/same", map[string]any{"paths": paths(entry("/same", "literal")), "equivalent_registrations": group})),
		node("route:src/a.js#GET%20%2Fhealth", "src/a.js", 3, express("GET", "/health", map[string]any{"paths": paths(entry("/health", "literal"))})),
		node("route:src/b.js#GET%20%2Fhealth", "src/b.js", 3, express("GET", "/health", map[string]any{"paths": paths(entry("/health", "literal"))})),
		node("route:src/users.js#GET%20%2F%3Aid", "src/users.js", 3, express("GET", "/:id", map[string]any{"paths": paths(entry("/users/{id}", "literal"))})),
		node("route:src/users.js#DELETE%20%2F%3AuserId", "src/users.js", 4, express("DELETE", "/:userId", map[string]any{"paths": paths(entry("/users/{userId}", "literal"))})),
		node("route:src/files.js#GET%20%2F*", "src/files.js", 3, express("GET", "/*", map[string]any{"paths": paths(entry("/files/{*}", "literal"))})),
		node("route:src/files.js#GET%20%2F%3Aname%3F", "src/files.js", 4, express("GET", "/:name?", map[string]any{"paths": paths(entry("/names/{name}", "literal", "optional", true))})),
		node("route:src/files.js#ALL%20%2Fany", "src/files.js", 5, express("ANY", "/any", map[string]any{"paths": paths(entry("/any", "literal"),
			entry("/test-only/any", "literal", "scope", "test"))})),
		node("route:src/files.js#GET%20%2Fdouble", "src/files.js", 6, express("GET", "/double", map[string]any{"paths": paths(entry("/a/{x}/b/{x}", "literal"))})),
		node("route:src/files.js#GET%20%2Ffuture", "src/files.js", 7, express("GET", "/future", map[string]any{"paths": paths(entry("/future", "configured"))})),
		node("route:app/auth.py#GET%20%2Flax", "app/auth.py", 10, fastapi("GET", "/lax", map[string]any{"paths": paths(entry("/lax", "literal")),
			"security_dependencies": []any{map[string]any{"class": "HTTPBearer", "subclass": "LaxBearer", "scheme_name": "LaxBearer", "auto_error": true, "instance": "app/security.py#lax", "chain": []any{}}}})),
		node("route:app/shops.py#GET%20%2F%7Bitem%7D", "app/shops.py", 3, fastapi("GET", "/{item}", map[string]any{"paths": paths(entry("/shops/{shop}/{item}", "literal"))})),
		node("route:app/items.py#GET%20%2Ffiles%2Fme", "app/items.py", 21, fastapi("GET", "/files/me", map[string]any{"paths": paths(entry("/files/me", "constant"))})),
		node("route:src/orders.js#GET%20%2F%3Aid", "src/orders.js", 3, express("GET", "/:id", map[string]any{"paths": paths(entry("/orders/{id}", "literal"))})),
		node("route:src/orders.js#GET%20%2Fme", "src/orders.js", 4, express("GET", "/me", map[string]any{"paths": paths(entry("/orders/me/", "literal"))})),
		node("route:src/orders.js#ALL%20%2Fstatic%2F*", "src/orders.js", 5, express("ANY", "/static/*", map[string]any{"paths": paths(entry("/static/{*}", "literal"))})),
		node("route:src/orders.js#GET%20%2Fstatic%2Fa%2Fb", "src/orders.js", 6, express("GET", "/static/a/b", map[string]any{"paths": paths(entry("/static/a/b", "literal"))})),
		node("route:src/orphan.js#GET%20%2Fdup", "src/orphan.js", 7, express("GET", "/dup", map[string]any{"paths": paths()})),
		node("route:src/orphan.js#GET%20%2Fequiv", "src/orphan.js", 8, express("GET", "/equiv", map[string]any{"paths": paths()})),
		hashed(node("route:src/orphan.js#GET%20%2Fw%402", "src/orphan.js", 9, express("GET", "/w", map[string]any{"paths": paths()})), "a"),
		hashed(node("route:src/orphan.js#GET%20%2Fv%402", "src/orphan.js", 10, express("GET", "/v", map[string]any{"paths": paths()})), "b"),
		hashed(node("route:src/orphan.js#GET%20%2Fu%402", "src/orphan.js", 11, express("GET", "/u", map[string]any{"paths": paths()})), "c"),
		hashed(node("route:src/twins.js#GET%20%2Ft", "src/twins.js", 3, express("GET", "/t", map[string]any{"paths": paths()})), "twin"),
		hashed(node("route:src/twins.js#GET%20%2Ft%402", "src/twins.js", 4, express("GET", "/t", map[string]any{"paths": paths()})), "twin"),
		hashed(node("route:src/orphan.js#GET%20%2Fs", "src/orphan.js", 12, express("GET", "/s", map[string]any{"paths": paths()})), "s"),
		node("route:app/f.py#GET%20%2Ff%2F%7Bp%3Apath%7D", "app/f.py", 3, fastapi("GET", "/f/{p:path}", map[string]any{"paths": paths(entry("/f/{p}", "literal"))})),
		node("route:app/f.py#GET%20%2Ff%2F", "app/f.py", 4, fastapi("GET", "/f/", map[string]any{"paths": paths(entry("/f", "literal", "trailing_slash", true))})),
		node("route:src/a.js#GET%20%2Fa%2F*", "src/a.js", 5, express("GET", "/a/*", map[string]any{"paths": paths(entry("/a/{*}", "literal"))})),
		node("route:src/a.js#GET%20%2Fa%2F", "src/a.js", 6, express("GET", "/a/", map[string]any{"paths": paths(entry("/a", "literal"))})),
		node("route:src/a.js#GET%20%2FUsers", "src/a.js", 7, express("GET", "/Users", map[string]any{"paths": paths(entry("/Users", "literal"))})),
		node("route:src/a.js#GET%20%2Fusers", "src/a.js", 8, express("GET", "/users", map[string]any{"paths": paths(entry("/users", "literal"))})),
		node("route:src/orphan.js#GET%20%2Florn", "src/orphan.js", 5, express("GET", "/lorn", map[string]any{"paths": paths()})),
		node("route:src/orphan.js#GET%20%2Fworkflows%2F%3Aid", "src/orphan.js", 6, express("GET", "/workflows/:id", map[string]any{"paths": paths()})),
		node("route:src/app.test.js#GET%20%2Ftest", "src/app.test.js", 3, express("GET", "/test", map[string]any{"paths": paths(entry("/test", "literal")), "scope": "test"})),
		node("route:src/main.rs#CONNECT%20%2Ftunnel", "src/main.rs", 9, map[string]any{"framework": "axum", "method": "CONNECT", "pattern": "/tunnel", "handler": nil,
			"handler_inline": false, "middleware": []any{}, "layers": []any{}, "paths": paths(entry("/tunnel", "literal"))}),
		node("route:cmd/web/main.go#GET%20example.com%2Fhost", "cmd/web/main.go", 7, map[string]any{"framework": "net/http", "method": "GET", "pattern": "GET example.com/host",
			"host": "example.com", "handler": nil, "handler_inline": false, "middleware": []any{}, "assumptions": []any{"GR-V2"}, "paths": paths(entry("/host", "literal"))}),
		node("route:cmd/web/main.go#GET%20%2Fwrapped", "cmd/web/main.go", 8, map[string]any{"framework": "net/http", "method": "GET", "pattern": "GET /wrapped",
			"handler": nil, "handler_inline": false, "middleware": []any{}, "assumptions": []any{"GR-V2"},
			"paths": paths(entry("/wrapped", "literal", "assumptions", []any{"GR-V5"}, "served_through", []any{"mw.Wrap"}))}),
		node("route:web/app/api/x/route.ts#GET%20%2Fapi%2Fx", "web/app/api/x/route.ts", 1, next("web", paths(entry("/api/x", "unresolved")))),
		node("route:docs/app/api/x/route.ts#GET%20%2Fapi%2Fx", "docs/app/api/x/route.ts", 1, next("docs", paths(entry("/api/x", "unresolved"), entry("/", "unresolved")))),
	}
	local, err := config.ParseBaseURL("http://127.0.0.1:8000")
	if err != nil {
		t.Fatal(err)
	}
	staging, err := config.ParseBaseURL("https://staging.example.com/api")
	if err != nil {
		t.Fatal(err)
	}
	servers := Servers(
		[]config.ProjectEnvironment{
			{Name: "shared", Label: "staging", BaseURLs: []config.MemberBaseURL{{Repository: "synthetic", BaseURL: staging}}, Line: 4},
			{Name: "other-only", Label: "local", BaseURLs: []config.MemberBaseURL{{Repository: "other", BaseURL: local}}, Line: 9},
		},
		[]config.Environment{{Name: "local", Label: "local", BaseURL: local, Auth: &config.Auth{Header: "Authorization", Env: "API_TOKEN"}, Line: 3}},
		"synthetic")
	return Input{
		Servers:    servers,
		Repository: "synthetic",
		Run:        &store.Run{ID: "RUN1", FinishedAt: "2026-11-20T00:00:00Z", Header: contract.Header{Indexer: contract.Indexer{Name: "graphvein", Version: "extractors-50"}, Commit: "abc123"}},
		Routes:     routes,
		Diagnostics: []contract.Diagnostic{
			{Type: "diagnostic", File: "src/orphan.js", Severity: "info", Code: "route_unmounted", Message: "orphanRouter at 5:0-5:30"},
			{Type: "diagnostic", File: "src/other.js", Severity: "info", Code: "route_unmounted", Message: "elsewhere at 5:0-5:30"},
		},
		Documents: []contract.Edge{{Type: "edge", Kind: "documents_route", Source: "api_operation:openapi.yaml#GET%20%2Fitems%2F%7Bid%7D", Target: "route:app/items.py#GET%20%2F%7Bid%7D", Provenance: "inferred", Confidence: 0.9}},
		Specs:     []contract.Node{{Identity: "api_spec:openapi.yaml#spec", Kind: "api_spec", File: "openapi.yaml", Attributes: map[string]any{"title": "Items", "spec_version": "3.1.0"}}},
		SpecOperations: []contract.Node{
			{Identity: "api_operation:openapi.yaml#GET%20%2Fitems%2F%7Bid%7D", Kind: "api_operation", File: "openapi.yaml", Attributes: map[string]any{"status": "linked"}},
			{Identity: "api_operation:openapi.yaml#GET%20%2Fgone", Kind: "api_operation", File: "openapi.yaml", Attributes: map[string]any{"status": "unmatched"}},
		},
		PathValues: []config.PathValue{
			{Key: "nextjs_base_path:docs", Value: "/docs"},
			{Key: "route:src/orphan.js#GET%20%2Fworkflows%2F%3Aid", Value: "/v2/workflows/{id}"},
			{Key: "route:src/a.js#GET%20%2Fhealth", Value: "/elsewhere"},
			{Key: "route:gone.js#GET%20%2Fx", Value: "/x"},
			{Key: "route:src/orphan.js#GET%20%2Fdup", Value: "/bearer"},
			{Key: "route:src/orphan.js#GET%20%2Fequiv", Value: "/api/items/{other}"},
			{Key: "route:src/orphan.js#GET%20%2Fw%402", Value: "/w", Registration: hash("a")},
			{Key: "route:src/orphan.js#GET%20%2Fv%402", Value: "/v", Registration: hash("other")},
			{Key: "route:src/orphan.js#GET%20%2Fu%402", Value: "/u"},
			{Key: "route:src/twins.js#GET%20%2Ft", Value: "/twin", Registration: hash("twin")},
			{Key: "route:src/orphan.js#GET%20%2Fs", Value: "/s", Registration: hash("not s")},
		},
	}
}

// TestGenerateSynthetic checks the document of the synthetic input
// against its golden and the rows' central claims directly.
func TestGenerateSynthetic(t *testing.T) {
	in := syntheticInput(t)
	doc := Generate(in)
	data, err := Encode(doc, FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	var got any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "testdata/synthetic.golden.json", got)

	// Determinism (OG-51): the same input in another order gives the same bytes.
	shuffled := syntheticInput(t)
	for i, j := 0, len(shuffled.Routes)-1; i < j; i, j = i+1, j-1 {
		shuffled.Routes[i], shuffled.Routes[j] = shuffled.Routes[j], shuffled.Routes[i]
	}
	again, err := Encode(Generate(shuffled), FormatJSON)
	if err != nil || string(again) != string(data) {
		t.Fatalf("the document depends on route order (%v)", err)
	}
	// OG-52: YAML is the same document.
	y, err := Encode(doc, FormatYAML)
	if err != nil {
		t.Fatal(err)
	}
	var fromYAML any
	if err := yaml.Unmarshal(y, &fromYAML); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(normalizeYAML(fromYAML), got) {
		t.Error("the YAML document differs from the JSON document")
	}
	if !strings.Contains(string(y), "\n  /api/items/:\n") && !strings.Contains(string(y), "'/api/items/':") {
		t.Errorf("YAML path keys not written as expected:\n%s", y)
	}

	op := func(key, method string) *Operation {
		t.Helper()
		item := doc.Paths[key]
		if item == nil {
			t.Fatalf("no path %s", key)
		}
		o := *item.slot(method)
		if o == nil {
			t.Fatalf("no %s %s", method, key)
		}
		return o
	}
	// OG-87: the shape's complete fact security replaces OG-30.
	get := op("/api/items/{id}", "GET")
	if !reflect.DeepEqual(get.Security, []map[string][]string{{"HTTPBearer": {}}}) || get.X.Resolution != "settings_default" {
		t.Errorf("GET /api/items/{id}: security %v resolution %s", get.Security, get.X.Resolution)
	}
	if len(get.X.DocumentedBy) != 1 || get.X.DocumentedBy[0].Confidence != 0.9 {
		t.Errorf("documented_by %v", get.X.DocumentedBy)
	}
	// OG-74..OG-77: only facts are standard.
	post := op("/api/items/", "POST")
	var names []string
	for _, p := range post.Parameters {
		names = append(names, p.In+":"+p.Name)
	}
	if strings.Join(names, ",") != "query:q,query:limit,header:X-Trace" {
		t.Errorf("POST parameters %v", names)
	}
	if post.Security != nil || !post.X.Conditional {
		t.Errorf("POST: partial security written (%v) or conditional lost", post.Security)
	}
	if post.RequestBody == nil || len(post.RequestBody.Content) != 2 || post.RequestBody.X["required"] != "not_read" {
		t.Errorf("POST body %+v", post.RequestBody)
	}
	if r := post.Responses["204"]; r == nil || r.Content != nil || r.X["body"] != "none" {
		t.Errorf("POST 204 %+v", r)
	}
	if _, ok := post.Responses["4XX"]; ok {
		t.Error("a fact response without content type is written")
	}
	if _, ok := post.Responses["404"]; ok {
		t.Error("a mapped response is written")
	}
	rows := map[string]int{}
	for _, it := range post.X.Shape.NotStandard {
		rows[it.Row]++
	}
	if !reflect.DeepEqual(rows, map[string]int{"OG-73": 1, "OG-75": 2, "OG-76": 1, "OG-77": 3, "OG-79": 1, "OG-82": 1, "OG-87": 1}) {
		t.Errorf("not_standard rows %v", rows)
	}
	if strings.Join(post.X.NotRead, ",") != "operationId,summary,tags" {
		t.Errorf("POST not_read %v", post.X.NotRead)
	}
	// OG-85: one definition per component, a colliding name numbered.
	if doc.Components == nil || doc.Components.Schemas["Item"] == nil || doc.Components.Schemas["Item_2"] == nil || len(doc.Components.Schemas) != 2 {
		t.Errorf("components %v", doc.Components)
	}
	// OG-87, OG-88.
	if k := op("/key", "GET"); !reflect.DeepEqual(k.Security, []map[string][]string{{"ApiKey": {}}, {}}) {
		t.Errorf("api key security %v", k.Security)
	}
	if f := op("/flows", "GET"); f.Security != nil {
		t.Error("OAuth2 without flows written")
	}
	if p := op("/public", "GET"); p.Security != nil || contains(p.X.NotRead, "security") {
		t.Errorf("provably none: security %v not_read %v", p.Security, p.X.NotRead)
	}
	// OG-30/OG-31: complete HTTP schemes only; a name with two
	// definitions is written by neither.
	if d := op("/digest", "GET"); !reflect.DeepEqual(d.Security, []map[string][]string{{"HTTPDigest": {}}}) {
		t.Errorf("digest %v", d.Security)
	}
	for _, k := range []string{"/oauth", "/optional", "/partial", "/bearer", "/basic"} {
		if o := op(k, "GET"); o.Security != nil || !contains(o.X.NotRead, "security") {
			t.Errorf("%s: security %v", k, o.Security)
		}
	}
	// The same scheme read from a shape and from a FastAPI class is one.
	if o := op("/same-scheme", "GET"); !reflect.DeepEqual(o.Security, []map[string][]string{{"HTTPBearer": {}}}) {
		t.Errorf("same scheme from two sources: %v", o.Security)
	}
	if _, ok := doc.Components.SecuritySchemes["Shared"]; ok {
		t.Error("conflicting scheme written")
	}
	// OG-70: an invalid shape is reported and read as absent.
	if b := op("/broken", "GET"); b.X.ShapeError == nil || b.X.Shape != nil || doc.X.ShapeErrors != 1 {
		t.Error("invalid shape not reported")
	}
	// OG-89.
	if c := op("/capped", "POST"); len(c.X.Shape.DepthCap) != 1 || c.X.Shape.Stops["depth_cap"] != 1 {
		t.Errorf("depth cap %+v", c.X.Shape)
	}
	// OG-18.
	if p := op("/files/{p}", "GET"); p.Parameters[0].X["matches"] == nil {
		t.Error("path convertor not marked")
	}
	// OG-20: an equivalent group is one operation; two apps are a conflict.
	if s := op("/same", "GET"); len(s.X.EquivalentRegistrations) != 2 || s.X.Route != "route:src/router.js#GET%20%2Fsame" {
		t.Errorf("equivalent group %v", s.X)
	}
	if _, ok := doc.Paths["/health"]; ok {
		t.Error("duplicate operation written")
	}
	// OG-92: values set by you, and never over a path read from code.
	if w := op("/v2/workflows/{id}", "GET"); w.X.Resolution != SetByYou || w.X.Assumptions[0].ID != SetByYou {
		t.Errorf("route value %+v", w.X)
	}
	if d := op("/docs/api/x", "GET"); d.X.Resolution != SetByYou {
		t.Errorf("base value %+v", d.X)
	}
	op("/docs", "GET")
	status := map[string]string{}
	for _, v := range doc.X.UserValues {
		status[v.Key] = v.Status
	}
	if status["route:src/a.js#GET%20%2Fhealth"] != UserValueUnused || status["route:gone.js#GET%20%2Fx"] != UserValueUnused || status["nextjs_base_path:docs"] != UserValueUsed {
		t.Errorf("user values %v", doc.X.UserValues)
	}
	// B-review fixes: subclass refused (OG-31), user value conflicts
	// refused on their own (OG-92), ordinal keys checked against their
	// registration (OG-90), convertor markers (OG-18), shadowing (OG-19).
	if o := op("/lax", "GET"); o.Security != nil {
		t.Error("a scheme subclass is written as its base class")
	}
	if b := op("/bearer", "GET"); b.X.Route != "route:app/auth.py#GET%20%2Fbearer" {
		t.Errorf("a value set by you replaced code: %+v", b.X)
	}
	if i := op("/api/items/{id}", "GET"); i.X.Resolution != "settings_default" {
		t.Error("an equivalent value set by you displaced code")
	}
	if _, ok := doc.Paths["/api/items/{other}"]; ok {
		t.Error("an equivalent value set by you was written")
	}
	vals := map[string]UserValue{}
	for _, v := range doc.X.UserValues {
		vals[v.Key] = v
	}
	for key, want := range map[string]string{"route:src/orphan.js#GET%20%2Fdup": UserValueUnused, "route:src/orphan.js#GET%20%2Fequiv": UserValueUnused,
		"route:src/orphan.js#GET%20%2Fw%402": UserValueUsed, "route:src/orphan.js#GET%20%2Fv%402": UserValueUnused, "route:src/orphan.js#GET%20%2Fu%402": UserValueUsed} {
		if v := vals[key]; v.Status != want || want == UserValueUnused && v.Why == "" {
			t.Errorf("%s: %+v", key, v)
		}
	}
	if vals["route:src/orphan.js#GET%20%2Fv%402"].Why != WhyRegistrationChanged || vals["route:src/orphan.js#GET%20%2Fu%402"].Warning != WarnNoRegistration || vals["route:src/orphan.js#GET%20%2Fw%402"].Warning != "" {
		t.Errorf("ordinal keys %+v", vals)
	}
	if v := vals["route:src/twins.js#GET%20%2Ft"]; v.Status != UserValueUnused || v.Why != WhyRegistrationTwin {
		t.Errorf("identical twin registration: %+v", v)
	}
	if v := vals["route:src/orphan.js#GET%20%2Fs"]; v.Status != UserValueUnused || v.Why != WhyRegistrationChanged {
		t.Errorf("unsuffixed key with a changed registration: %+v", v)
	}
	if v := vals["route:src/orphan.js#GET%20%2Fworkflows%2F%3Aid"]; v.Status != UserValueUsed || v.Warning != WarnNoRegistration {
		t.Errorf("route key without a registration: %+v", v)
	}
	if p := op("/shops/{shop}/{item}", "GET"); p.Parameters[0].X["matches"] != MatchesNotRead || p.Parameters[1].X["matches"] != nil {
		t.Errorf("prefix parameter %+v", p.Parameters)
	}
	shadowed := func(key string) []string {
		var out []string
		for _, s := range op(key, "GET").X.MayBeShadowed {
			out = append(out, s.Key)
		}
		return out
	}
	if got := shadowed("/orders/me/"); !reflect.DeepEqual(got, []string{"/orders/{id}"}) {
		t.Errorf("/orders/me/ shadowed by %v", got)
	}
	if got := shadowed("/orders/{id}"); got != nil {
		t.Errorf("/orders/{id} shadowed by %v", got)
	}
	if got := shadowed("/static/a/b"); !reflect.DeepEqual(got, []string{"/static/{*}"}) {
		t.Errorf("/static/a/b shadowed by %v", got)
	}
	if got := shadowed("/files/me"); !reflect.DeepEqual(got, []string{"/files/{p}"}) {
		t.Errorf("/files/me shadowed by %v", got)
	}
	// An empty rest: Starlette's {p:path} takes /f/, Express 4's * takes
	// /a/ (trailing slash ignored); Express literals compare without case.
	if got := shadowed("/f/"); !reflect.DeepEqual(got, []string{"/f/{p}"}) {
		t.Errorf("/f/ shadowed by %v", got)
	}
	if got := shadowed("/a"); !reflect.DeepEqual(got, []string{"/a/{*}"}) {
		t.Errorf("/a shadowed by %v", got)
	}
	if got := shadowed("/users"); !reflect.DeepEqual(got, []string{"/Users"}) {
		t.Errorf("/users shadowed by %v", got)
	}
	if got := shadowed("/wrapped"); got != nil {
		t.Errorf("a most-specific framework lists shadowing: %v", got)
	}
	reasons := map[string]bool{}
	for _, u := range doc.X.Unrepresentable {
		reasons[u.Reason] = true
	}
	for _, r := range []string{ReasonWildcard, ReasonOptional, ReasonHost, ReasonMethod, ReasonDuplicateParam, ReasonResolution} {
		if !reasons[r] {
			t.Errorf("no unrepresentable entry with reason %s", r)
		}
	}
	conflicts := map[string]bool{}
	for _, c := range doc.X.Conflicts {
		conflicts[c.Reason] = true
	}
	if len(conflicts) != 3 {
		t.Errorf("conflicts %v", doc.X.Conflicts)
	}
	if doc.X.ExcludedTest != (ExcludedTest{Routes: 1, PathEntries: 1}) {
		t.Errorf("excluded %+v", doc.X.ExcludedTest)
	}
	unresolved := map[string]Unresolved{}
	for _, u := range doc.X.Unresolved {
		unresolved[u.Route] = u
	}
	if u := unresolved["route:src/orphan.js#GET%20%2Florn"]; u.Unknown.Diagnostic != "route_unmounted" || u.Unknown.Expression != "orphanRouter" {
		t.Errorf("unknown part %+v", u)
	}
	if u := unresolved["route:web/app/api/x/route.ts#GET%20%2Fapi%2Fx"]; u.Reason != UnresolvedBase || len(u.Keys) != 2 || u.Keys[1] != "nextjs_base_path:web" {
		t.Errorf("unresolved base %+v", u)
	}
	// OG-5: configured environments only, project ones first, no auth.
	if len(doc.Servers) != 2 || doc.Servers[0].URL != "https://staging.example.com/api" || doc.Servers[1].URL != "http://127.0.0.1:8000" ||
		strings.Contains(string(data), "API_TOKEN") || doc.X.Index.Run != "RUN1" {
		t.Errorf("servers %+v or run lost", doc.Servers)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// normalizeYAML turns yaml.v3's decoded maps and numbers into what
// encoding/json decodes, so the two documents compare.
func normalizeYAML(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			x[k] = normalizeYAML(e)
		}
		return x
	case []any:
		for i, e := range x {
			x[i] = normalizeYAML(e)
		}
		return x
	case int:
		return float64(x)
	}
	return v
}

// TestEmptyDocument: a repository without routes is still a valid
// document with empty paths, and an unknown format is refused.
func TestEmptyDocument(t *testing.T) {
	doc := Generate(Input{Repository: "empty"})
	if len(doc.Paths) != 0 || doc.Components != nil || doc.X.Index != nil {
		t.Errorf("%+v", doc)
	}
	data, err := Encode(doc, FormatJSON)
	if err != nil || !strings.Contains(string(data), `"paths": {}`) {
		t.Errorf("%s %v", data, err)
	}
	if _, err := Encode(doc, "xml"); err == nil {
		t.Error("xml accepted")
	}
}

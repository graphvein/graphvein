package clients

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/scan"
)

func TestOrvalNamesAndVersion(t *testing.T) {
	for _, c := range []struct{ input, camel, kebab string }{
		{"GetCurrentUser", "getCurrentUser", "get-current-user"},
		{"USER_AUTH", "userAuth", "user-auth"},
		{"User Authentication", "userAuthentication", "user-authentication"},
		{"UserAuth", "userAuth", "user-auth"},
		{"get-article", "getArticle", "get-article"},
		{"_USER_NAME", "_userName", "user-name"},
	} {
		if orvalCamel(c.input) != c.camel || orvalKebab(c.input) != c.kebab {
			t.Fatal(c, orvalCamel(c.input), orvalKebab(c.input))
		}
	}
	for _, s := range []string{"8", "8.x", "8.3.0", "^8.3.0", "~8.3.0", "=v8.3.0"} {
		if !orvalMajor.MatchString(s) {
			t.Fatal("major 8 rejected", s)
		}
	}
	for _, s := range []string{"^7.0.0", "9", "8 || 9", ">=8", "latest", "workspace:^8", "file:8", "80.0.0"} {
		if orvalMajor.MatchString(s) {
			t.Fatal("unproven range accepted", s)
		}
	}
}

func TestOrvalOptionsIndexUsesParameterGroups(t *testing.T) {
	literal := func(s string) expr { return expr{Kind: "literal", Name: s} }
	sources := map[string][]byte{"package.json": []byte(`{"devDependencies":{"orval":"8"}}`), "api.yaml": nil}
	r := &resolver{files: map[string]facts.Facts{}}
	config := expr{Kind: "object", Properties: map[string]expr{"input": literal("api.yaml"), "output": {Kind: "object", Properties: map[string]expr{"client": literal("react-query"), "mode": literal("tags-split"), "target": literal("generated")}}}}
	for _, c := range []struct {
		locations []string
		content   []string
		want      int
	}{
		{nil, nil, 0}, {[]string{"path", "path", "query", "query", "header"}, nil, 4},
		{nil, []string{"application/json"}, 1}, {[]string{"body"}, nil, 1},
		{[]string{"formData", "formData"}, nil, 1}, {[]string{"body", "query", "header"}, []string{"application/json"}, 3},
	} {
		params := []map[string]any{}
		for _, where := range c.locations {
			params = append(params, map[string]any{"in": where})
		}
		specs := []contract.Node{{Kind: "api_spec", File: "api.yaml", Attributes: map[string]any{"version_supported": true}}, {Kind: "api_operation", File: "api.yaml", Attributes: map[string]any{"operation_id": "Read", "method": "GET", "path": "/t012e/read", "parameters": params, "request_content_types": c.content}}}
		project, reason, _ := r.orvalModel("orval.config.ts", "fetch", config, sources, specs)
		if reason != "" || len(project.operations) != 1 || project.operations[0].optionsIndex != c.want {
			t.Fatal(c, reason, project.operations)
		}
	}
}

func TestOrvalParameterNames(t *testing.T) {
	literal := func(s string) expr { return expr{Kind: "literal", Name: s} }
	// A same-named config constant must not replace a generated parameter.
	bindings := map[string]facts.RouteBinding{}
	for _, name := range []string{"slug", "userName", "articleId", "undefined"} {
		bindings[name] = facts.RouteBinding{Constant: true, Value: literal("fixed"), Evaluation: &facts.URLBinding{Constant: true, Value: literal("fixed")}}
	}
	scope := facts.RouteScope{Parent: -1, Bindings: bindings}
	r := &resolver{files: map[string]facts.Facts{"orval.config.ts": {Routes: &facts.RouteFacts{Scopes: []facts.RouteScope{scope}, URLScopes: []facts.RouteScope{scope}}}}}
	sources := map[string][]byte{"package.json": []byte(`{"devDependencies":{"orval":"8"}}`)}
	config := expr{Kind: "object", Properties: map[string]expr{"input": literal("api.yaml"), "output": {Kind: "object", Properties: map[string]expr{"client": literal("react-query"), "mode": literal("tags-split"), "target": literal("generated")}}}}
	for _, tc := range []struct{ raw, want string }{
		{"/t012e/articles/{slug}", "/t012e/articles/{slug}"},
		{"/t012e/{USER_NAME}/{article-id}", "/t012e/{userName}/{articleId}"},
		{"/t012e/{undefined}", "/t012e/{undefined}"},
	} {
		specs := []contract.Node{{Kind: "api_spec", File: "api.yaml", Attributes: map[string]any{"version_supported": true}}, {Kind: "api_operation", File: "api.yaml", Attributes: map[string]any{"operation_id": "Read", "method": "GET", "path": tc.raw}}}
		project, reason, _ := r.orvalModel("orval.config.ts", "fetch", config, sources, specs)
		if reason != "" || len(project.operations) != 1 {
			t.Fatal(tc, reason, project.operations)
		}
		attrs, _, _, reason := r.orvalTransport(project, project.operations[0])
		if reason != "" {
			t.Fatal(tc, reason)
		}
		paths := attrs["paths"].([]map[string]any)
		if len(paths) != 1 || paths[0]["template"] != tc.want {
			t.Fatal(tc, paths)
		}
	}
}

func TestGeneratedIgnoreProvenanceMatchesScanner(t *testing.T) {
	for _, c := range []struct{ directory, pattern string }{
		{"generated", "generated/"}, {"generated", "generated\n!generated"},
		{"a/generated", "**/generated/"}, {"a/b/generated", "a**generated/"},
		{"a/generated", "/a/gen?rated/"}, {"generated", "g[!a]nerated/"},
		{"!generated", `\!generated/`}, {"#generated", `\#generated/`},
		{"visible", "__orval_directory__"},
	} {
		root := t.TempDir()
		file := filepath.Join(root, filepath.FromSlash(c.directory), "source.ts")
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("export const source = 1;\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(c.pattern+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		result, err := scan.Walk(context.Background(), root)
		if err != nil {
			t.Fatal(err)
		}
		visible := false
		for _, f := range result.Files {
			visible = visible || f.Record.Path == c.directory+"/source.ts"
		}
		source := generatedIgnoreSource(c.directory, map[string][]byte{".gitignore": []byte(c.pattern)})
		if (source == "") != visible {
			t.Fatalf("%+v: scanner visible=%v, provenance=%q", c, visible, source)
		}
	}
}

func TestAssignedURLOriginNeverSuffixMatches(t *testing.T) {
	m := newMatcher([]contract.Node{{Kind: "route", Identity: "route:server.ts#items", Attributes: map[string]any{"method": "GET", "paths": []map[string]any{{"template": "/api/items"}}}}})
	attrs := map[string]any{"method": "GET", "path_template": "/items", "base": "unresolved", "origin": nil, "origin_resolution": "unresolved"}
	if edges := m.link("client_call:app.ts#items", "app.ts", contract.Span{}, attrs); len(edges) != 0 || attrs["status"] != "unmatched" {
		t.Fatal("assigned authority used suffix tier", attrs, edges)
	}
	attrs["path_template"] = "/api/items"
	if edges := m.link("client_call:app.ts#items", "app.ts", contract.Span{}, attrs); len(edges) != 1 || edges[0].Confidence != .7 {
		t.Fatal("assigned authority lost exact origin tier", attrs, edges)
	}
}

func TestGeneratedAbsentExplainsVisibility(t *testing.T) {
	root := t.TempDir()
	for _, c := range []struct{ dir, rule, text, want string }{
		{"missing", ".gitignore", "missing/", "does not exist"},
		{"generated", ".gitignore", "generated/", ".gitignore"},
		{"custom", ".graphveinignore", "custom/", ".graphveinignore"},
		{"node_modules/output", "", "", "built-in"},
		{"visible", ".gitignore", "visible/\n!visible/", "exists but has no scanned files"},
	} {
		if c.dir != "missing" {
			if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(c.dir)), 0755); err != nil {
				t.Fatal(err)
			}
		}
		message := generatedAbsent(root, c.dir, map[string][]byte{c.rule: []byte(c.text)})
		if !strings.Contains(message, c.want) {
			t.Fatal(c, message)
		}
		if c.dir == "missing" && strings.Contains(message, "negation") {
			t.Fatal("absent output has ignore remedy", message)
		}
	}
}

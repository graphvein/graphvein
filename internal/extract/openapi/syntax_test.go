package openapi

import (
	"fmt"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/testscope"
)

// Declaration tests keep their diagnostic assertions independent of coverage,
// which is checked with route outcomes by the index fixture tests.
func declarationLink(sources map[string][]byte, rules, tests testscope.Rules) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	nodes, edges, diagnostics := Link(sources, rules, tests)
	var declarations []contract.Diagnostic
	for _, d := range diagnostics {
		if d.Code != "openapi_coverage" {
			declarations = append(declarations, d)
		}
	}
	return nodes, edges, declarations
}

func TestRevisionOneSpans(t *testing.T) {
	for _, tc := range []struct {
		name, source, path, method, want string
		json                             bool
	}{
		{"json escape", `{"openapi":"3.1.0","paths":{"/x":{"get":{"summary":"\u0078"}, "post":{}}}}`, "/x", "get", `"get":{"summary":"\u0078"}`, true},
		{"json nonascii", `{"openapi":"3.1.0","paths":{"/x":{"é":0, "get":{}}}}`, "/x", "get", `"get":{}`, true},
		{"flow comment", "openapi: 3.0.0\npaths:\n  /x:\n    get: {summary: x} # tail\n    post: {}\n", "/x", "get", "get: {summary: x} # tail", false},
		{"block scalar", "openapi: 3.0.0\npaths:\n  /x:\n    get:\n      description: |\n        # content\n    post: {}\n", "/x", "get", "get:\n      description: |\n        # content", false},
		{"between comment", "openapi: 3.0.0\npaths:\n  /x:\n    get: {}\n    # next operation\n    post: {}\n", "/x", "get", "get: {}\n    # next operation", false},
		{"eof", "openapi: 3.0.0\npaths:\n  /x:\n    get: {}", "/x", "get", "get: {}", false},
		{"yaml nonascii", "openapi: 3.0.0\npaths: {/x: {é: 0, get: {}, post: {}}}\n", "/x", "get", "get: {}", false},
		{"last flow", "openapi: 3.0.0\npaths: {/x: {get: {}}}\n", "/x", "get", "get: {}}}", false},
		{"alias", "openapi: 3.0.0\nx-op: &op {summary: x}\npaths:\n  /x:\n    get: *op\n", "/x", "get", "get: *op", false},
		{"path alias", "openapi: 3.0.0\nx-item: &item {get: {summary: x}}\npaths:\n  /x: *item\n", "/x", "get", "*item", false},
		{"flow path alias", "openapi: 3.0.0\nx-item: &item {get: {summary: x}}\npaths: {/x: *item, /next: {get: {}}}\n", "/x", "get", "*item", false},
		{"paths alias", "openapi: 3.0.0\nx-paths: &paths {/x: {get: {}}}\npaths: *paths\n", "/x", "get", "*paths", false},
		{"merge", "openapi: 3.0.0\nx-item: &item {get: {summary: x}}\npaths:\n  /x:\n    <<: *item\n", "/x", "get", "<<: *item", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, err := parseSource([]byte(tc.source), tc.json)
			if err != nil {
				t.Fatal(err)
			}
			e, ok := root.get("paths").get(tc.path).field(tc.method)
			if !ok {
				t.Fatal("missing entry")
			}
			if got := tc.source[e.start:e.end]; got != tc.want {
				t.Fatalf("span = %q, want %q", got, tc.want)
			}
			s := span([]byte(tc.source), e.start, e.end)
			line := strings.Split(tc.source, "\n")[s.StartLine-1]
			if !strings.HasPrefix(line[s.StartColumn:], tc.want) && !strings.HasPrefix(tc.want, line[s.StartColumn:]) {
				t.Fatal("invalid byte column", s)
			}
		})
	}
}

func TestPathSpellingAndCap(t *testing.T) {
	source := "openapi: 3.0.0\nservers:\n"
	for i := 0; i < 12; i++ {
		source += fmt.Sprintf("  - url: https://example.com/é/%02d/../:literal/%%2f\n", i)
	}
	source += "paths: {/x/: {get: {}}}\n"
	nodes, _, ds := declarationLink(map[string][]byte{"api.yaml": []byte(source)}, testscope.Rules{}, testscope.Rules{})
	if len(ds) != 0 || len(nodes) != 2 {
		t.Fatal(ds, nodes)
	}
	a := nodes[1].Attributes
	paths := a["paths"].([]map[string]any)
	if a["path_count"] != 12 || len(paths) != 10 || paths[0]["template"] != "/é/00/../:literal/%2f/x" || paths[0]["trailing_slash"] != true {
		t.Fatal(a)
	}
}

func TestStrictParsingAndDetection(t *testing.T) {
	for _, source := range []string{`{"a":1,"a":2}`, `{} {}`, "a: 1\na: 2\n", "a: 1\n---\nb: 2\n"} {
		if _, err := parseSource([]byte(source), strings.HasPrefix(source, "{")); err == nil {
			t.Fatalf("accepted %q", source)
		}
	}
	nodes, _, diagnostics := declarationLink(map[string][]byte{"ordinary.json": []byte(`{"nested":{"openapi":"3.0.0"}}`), "broken.json": []byte(`{"openapi":`), "broken.yaml": []byte("'openapi': ["), "swagger.JSON": []byte(`{"swagger":2.0}`)}, testscope.Rules{}, testscope.Rules{})
	if len(nodes) != 1 || len(diagnostics) != 1 {
		t.Fatalf("nodes=%d diagnostics=%v", len(nodes), diagnostics)
	}
}

func TestRevisionTwoVersions(t *testing.T) {
	for _, tc := range []struct {
		name, source, version, code string
		json, supported             bool
	}{
		{"yaml swagger float", "swagger: 2.0", "2.0", "", false, true},
		{"yaml swagger quoted", "swagger: '2.0'", "2.0", "", false, true},
		{"yaml openapi float", "openapi: 3.1", "3.1", "", false, true},
		{"json openapi number", `{"openapi":3.1}`, "3.1", "", true, true},
		{"json swagger number", `{"swagger":2.0}`, "2.0", "", true, true},
		{"written numeric spelling", "openapi: 3.10", "3.10", "openapi_unsupported_version", false, false},
		{"future minor", "openapi: 3.3", "3.3", "openapi_unsupported_version", false, false},
		{"components", "openapi: 3.1.2.3", "3.1.2.3", "", false, true},
		{"integer", "openapi: 3", "", "openapi_unsupported_version", false, false},
		{"other major", "openapi: 4.0.0", "", "openapi_unsupported_version", false, false},
		{"old version", "openapi: 1.2", "", "openapi_unsupported_version", false, false},
		{"swagger other version", "swagger: 3.0", "", "openapi_unsupported_version", false, false},
		{"mapping", "openapi: {include: [api.yaml]}", "", "", false, false},
		{"bool", "openapi: true", "", "", false, false},
		{"text", "openapi: enabled", "", "", false, false},
		{"null", "openapi: null", "", "", false, false},
		{"sequence", "openapi: [3.1]", "", "", false, false},
		{"bad component", "openapi: 3.1.x", "", "", false, false},
		{"exponent", `{"openapi":3.1e0}`, "", "", true, false},
		{"json null", `{"openapi":null}`, "", "", true, false},
		{"json bool", `{"openapi":true}`, "", "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := "spec.yaml"
			if tc.json {
				file = "spec.json"
			}
			nodes, _, ds := declarationLink(map[string][]byte{file: []byte(tc.source)}, testscope.Rules{}, testscope.Rules{})
			if tc.version == "" {
				if len(nodes) != 0 {
					t.Fatal(nodes)
				}
			} else if len(nodes) != 1 || nodes[0].Attributes["spec_version"] != tc.version || nodes[0].Attributes["version_supported"] != tc.supported {
				t.Fatal(nodes)
			}
			if tc.code == "" && len(ds) != 0 || tc.code != "" && (len(ds) != 1 || ds[0].Code != tc.code) {
				t.Fatal(ds)
			}
			if tc.version == "" && tc.code == "" {
				rules, err := testscope.Compile([]string{file}, nil)
				if err != nil {
					t.Fatal(err)
				}
				_, _, ds = declarationLink(map[string][]byte{file: []byte(tc.source)}, rules, testscope.Rules{})
				if len(ds) != 1 || ds[0].Code != "openapi_not_a_spec" || strings.Contains(ds[0].Message, "<nil>") {
					t.Fatal(ds)
				}
			}
		})
	}
}

func TestRevisionTwoDiagnosticText(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{"openapi: 3.1\n---\nopenapi: 3.2\n", "found 2 YAML documents"},
		{"openapi: 3.1\n---\n{}\n---\n{}", "found 3 YAML documents"},
		{`{"openapi":"3.1"} {}`, "found additional JSON content"},
	} {
		_, err := parseSource([]byte(tc.source), strings.HasPrefix(tc.source, "{"))
		if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "<nil>") {
			t.Fatalf("got %v, want %s", err, tc.want)
		}
	}
	rules, err := testscope.Compile([]string{"included.yaml"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, ds := declarationLink(map[string][]byte{"included.yaml": []byte("info: {}")}, rules, testscope.Rules{})
	if len(ds) != 1 || !strings.Contains(ds[0].Message, "missing version value") {
		t.Fatal(ds)
	}
	sources := map[string][]byte{"root.yaml": []byte("openapi: 3.2\npaths: {/x: {$ref: 'https://example.invalid/api#/paths/~1x'}}")}
	_, _, ds = declarationLink(sources, testscope.Rules{}, testscope.Rules{})
	if len(ds) != 1 || !strings.Contains(ds[0].Message, "not claimed by any scanned document") || strings.Contains(ds[0].Message, "claimants:") {
		t.Fatal(ds)
	}
	for _, name := range []string{"a.yaml", "b.yaml"} {
		sources[name] = []byte("openapi: 3.2\n$self: https://example.invalid/api\npaths: {}")
	}
	_, _, ds = declarationLink(sources, testscope.Rules{}, testscope.Rules{})
	if len(ds) != 1 || !strings.Contains(ds[0].Message, "claimants: a.yaml, b.yaml") {
		t.Fatal(ds)
	}
}

func TestPathsAndUnresolvedReferences(t *testing.T) {
	source := `openapi: 3.2.0
$self: ../outside.yaml
servers:
  - url: https://b.example/{v}
    variables: {v: {default: api}}
  - url: https://a.example/api
  - url: https://b.example/api
  - url: //c.example/api
  - url: relative
  - url: /{missing}
paths:
  /items/:
    get:
      parameters: [{$ref: '#/absent'}]
      responses: {'200': {$ref: https://remote.invalid/x}}
`
	nodes, _, ds := declarationLink(map[string][]byte{"spec.yaml": []byte(source)}, testscope.Rules{}, testscope.Rules{})
	if len(nodes) != 2 || len(ds) != 3 {
		t.Fatalf("nodes=%v diagnostics=%v", nodes, ds)
	}
	a := nodes[1].Attributes
	paths := a["paths"].([]map[string]any)
	if len(paths) != 2 || paths[0]["template"] != "/api/items" || paths[0]["trailing_slash"] != true || paths[0]["resolution"] != "literal" || len(paths[0]["origins"].([]string)) != 3 || paths[1]["template"] != nil {
		t.Fatal(paths)
	}
}

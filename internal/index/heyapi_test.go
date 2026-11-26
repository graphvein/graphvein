package index_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

func TestHeyAPIGoldenAndReplay(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/heyapi")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	data, first, _ := emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/heyapi.golden.json", store.Snapshot{Nodes: snapshot.Nodes, Edges: snapshot.Edges, Diagnostics: snapshot.Diagnostics})
	checkHeyAPIFixture(t, snapshot, false)
	warmData, warm, writes := emit(t, root, s, opt)
	if string(data) != string(warmData) || warm.Parsed != 0 || warm.Reused != first.Parsed || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0 {
		t.Fatal("hey-api cache replay differs", warm, writes)
	}
	if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte("version: 1\nclients:\n  base_paths:\n    conditional: /t012c/configured\n    double: /t012c/configured\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, configured, _ := emit(t, root, s, opt)
	if configured.Parsed != 0 {
		t.Fatal("base configuration reparsed source")
	}
	snapshot, err = s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	checkHeyAPIFixture(t, snapshot, true)
	// A configuration-only transport edit must withdraw proof on cache replay.
	config := filepath.Join(root, "main", "openapi-ts.config.ts")
	source, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte(strings.ReplaceAll(string(source), "@hey-api/client-axios", "@hey-api/client-fetch")), 0644); err != nil {
		t.Fatal(err)
	}
	_, changed, _ := emit(t, root, s, opt)
	if changed.Parsed != 1 {
		t.Fatal("transport edit should parse only config", changed)
	}
	snapshot, err = s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range snapshot.Nodes {
		if n.Kind == "client_call" && strings.HasPrefix(n.File, "main/") {
			t.Fatal("client-fetch retained axios proof", n)
		}
	}
}

func checkHeyAPIFixture(t *testing.T, s store.Snapshot, configured bool) {
	t.Helper()
	calls := 0
	for _, n := range s.Nodes {
		if n.Kind != "client_call" {
			continue
		}
		calls++
		if n.Attributes["library"] != "hey-api" {
			t.Fatal("unexpected transport", n)
		}
		p := n.Attributes["paths"].([]any)[0].(map[string]any)
		name := n.QualifiedName
		switch {
		case strings.HasPrefix(name, "Service.item."):
			if p["template"] != "/t012c/new/t012c/hey-items/{id}" || p["status"] != "linked" || p["match"] != "exact" {
				t.Fatal("setConfig must override initializer", n)
			}
		case strings.HasPrefix(name, "Service.slash."):
			if p["template"] != "/t012c/new/t012c/hey-items" || p["trailing_slash"] != true || p["status"] != "linked" {
				t.Fatal("trailing slash", n)
			}
		case strings.HasPrefix(name, "Service.perCall."):
			if p["template"] != "/t012c/per-call/t012c/hey-items" || p["status"] != "linked" {
				t.Fatal("per-call base precedence", n)
			}
		case n.File == "preserved/generated/sdk.gen.ts":
			if p["template"] != "/t012c/preserved/t012c/hey-items" || p["status"] != "linked" {
				t.Fatal("header-only setConfig must preserve initializer", n)
			}
		case strings.HasPrefix(n.File, "conditional/") || strings.HasPrefix(n.File, "double/"):
			want := "unresolved"
			if configured {
				want = "configured"
			}
			if n.Attributes["base"] != want {
				t.Fatal("conditional/multiple config bases", n)
			}
		default:
			if p["template"] != nil || p["status"] != "dynamic" {
				t.Fatal("unsupported request must remain dynamic", n)
			}
		}
	}
	if calls != 11 {
		t.Fatalf("hey-api calls=%d, want 11", calls)
	}
	counts := map[string]int{}
	for _, d := range s.Diagnostics {
		counts[d.Code]++
	}
	if counts["generated_client_absent"] != 1 || counts["generated_client_unproven"] != 4 || counts["generated_client_override"] != 1 {
		t.Fatal("generated diagnostics", counts)
	}
	for _, e := range s.Edges {
		if e.Kind == "calls_route" && e.Confidence != 0.9 {
			t.Fatal("exact confidence changed", e)
		}
	}
}

func TestHeyAPIConfigWritePrecision(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/heyapi")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	settings := filepath.Join(root, "main", "settings.ts")
	for _, source := range []string{
		"enabled && client.setConfig({ baseURL: '/t012c/new' });",
		"enabled || client.setConfig({ baseURL: '/t012c/new' });",
		"for (const value of values) client.setConfig({ baseURL: '/t012c/new' });",
		"function configure() { client.setConfig({ baseURL: '/t012c/new' }); }",
		"client.setConfig({ ...options, baseURL: '/t012c/new' });",
		"client.setConfig(options);",
		"client.setConfig({ baseURL: '/t012c/old', baseURL: '/t012c/new' });",
		"client.instance.defaults.baseURL = '/t012c/new';",
		"Object.assign(client.instance.defaults, { baseURL: '/t012c/new' });",
		"delete client.instance.defaults.baseURL;",
	} {
		t.Run(source, func(t *testing.T) {
			if err := os.WriteFile(settings, []byte("import { client } from './generated/client.gen';\n"+source+"\n"), 0644); err != nil {
				t.Fatal(err)
			}
			emit(t, root, s, opt)
			snapshot, err := s.Dump(context.Background(), "fixture")
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, n := range snapshot.Nodes {
				if n.Kind == "client_call" && strings.HasPrefix(n.QualifiedName, "Service.item.") {
					found = true
					if n.Attributes["base"] != "unresolved" {
						t.Fatal("unproven write supplied a base", n)
					}
				}
			}
			if !found {
				t.Fatal("missing generated request")
			}
		})
	}
}

func TestHeyAPIRequestAndFactoryPrecision(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/heyapi")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	sdk := filepath.Join(root, "main", "generated", "sdk.gen.ts")
	if err := os.WriteFile(sdk, []byte(`import { client } from './client.gen';
export function run() {
  use((options: Options) => (options.client ?? client).get({ url: '/t012c/hey-items/{id}', ...options }));
  client.get({ url: 'https://other/t012c/hey-items' });
  client.get({ url: '//other/t012c/hey-items' });
  const base = '/t012c/per-call';
  client.get({ url: itemURL(), baseURL: base });
}
function itemURL() { return '/t012c/hey-items'; }
`), 0644); err != nil {
		t.Fatal(err)
	}
	emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, n := range snapshot.Nodes {
		if n.Kind != "client_call" || n.File != "main/generated/sdk.gen.ts" {
			continue
		}
		count++
		p := n.Attributes["paths"].([]any)[0].(map[string]any)
		switch n.Span.StartLine {
		case 3:
			if p["template"] != "/t012c/new/t012c/hey-items/{id}" || p["status"] != "linked" {
				t.Fatal("anonymous arrow parameter proof", n)
			}
		case 4:
			if p["template"] != "/t012c/new/https:/other/t012c/hey-items" || p["origin"] != nil {
				t.Fatal("absolute URL must be a concatenated path", n)
			}
		case 5:
			if p["template"] != "/t012c/new/other/t012c/hey-items" || p["origin"] != nil {
				t.Fatal("base must not discard protocol-relative host", n)
			}
		case 7:
			if p["template"] != "/t012c/per-call/t012c/hey-items" || p["status"] != "linked" || p["resolution"] != "evaluated" || n.Attributes["helpers"] == nil || n.Attributes["base"] != "constant" {
				t.Fatal("bounded URL/helper evidence and local base scope", n)
			}
		}
	}
	if count != 4 {
		t.Fatal("request count", count)
	}
	config := filepath.Join(root, "main", "openapi-ts.config.ts")
	configSource, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte(strings.Replace(string(configSource), "path: './generated'", "path: '/generated'", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	emit(t, root, s, opt)
	snapshot, err = s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range snapshot.Nodes {
		if n.Kind == "client_call" && strings.HasPrefix(n.File, "main/") {
			t.Fatal("absolute output must not prove a repository-relative client", n)
		}
	}
	if err := os.WriteFile(config, configSource, 0644); err != nil {
		t.Fatal(err)
	}
	// A local function with the right name is not an imported transport factory.
	if err := os.WriteFile(filepath.Join(root, "main", "generated", "client.gen.ts"), []byte("const createClient = () => ({});\nexport const client = createClient();\n"), 0644); err != nil {
		t.Fatal(err)
	}
	emit(t, root, s, opt)
	snapshot, err = s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range snapshot.Nodes {
		if n.Kind == "client_call" && strings.HasPrefix(n.File, "main/") {
			t.Fatal("local factory invented transport proof", n)
		}
	}
}
